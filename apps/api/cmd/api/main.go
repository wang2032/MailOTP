package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"mailotp/apps/api/internal/config"
	"mailotp/apps/api/internal/otp"
	"mailotp/apps/api/internal/store"
)

const aliasAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

var aliasPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,99}$`)

type server struct {
	cfg   config.Config
	store *store.Store
}

type inboxCreateRequest struct {
	Alias  string `json:"alias"`
	UserID string `json:"user_id"`
	Label  string `json:"label"`
}

type mailRequest struct {
	Alias             string `json:"alias"`
	Recipient         string `json:"recipient"`
	Sender            string `json:"sender"`
	Subject           string `json:"subject"`
	Code              string `json:"code"`
	Content           string `json:"content"`
	ProviderMessageID string `json:"provider_message_id"`
}

type inboxResponse struct {
	ID        string          `json:"id"`
	UserID    string          `json:"user_id,omitempty"`
	Alias     string          `json:"alias"`
	Label     string          `json:"label,omitempty"`
	Email     string          `json:"email"`
	CreatedAt time.Time       `json:"created_at"`
	Messages  []store.Message `json:"messages"`
}

type latestResponse struct {
	Alias   string     `json:"alias"`
	Email   string     `json:"email"`
	Code    *string    `json:"code"`
	Time    *time.Time `json:"time"`
	Sender  string     `json:"sender,omitempty"`
	Subject string     `json:"subject,omitempty"`
}

type configResponse struct {
	MailDomain string `json:"mail_domain"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func main() {
	ctx := context.Background()
	cfg := config.Load()

	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer st.Close()

	if cfg.AutoCreateTables {
		if err := st.CreateTables(ctx); err != nil {
			log.Fatalf("create tables: %v", err)
		}
	}

	srv := &server{cfg: cfg, store: st}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", srv.health)
	mux.HandleFunc("/api/config", srv.getConfig)
	mux.HandleFunc("/api/inboxes", srv.createInbox)
	mux.HandleFunc("/api/inboxes/", srv.getInbox)
	mux.HandleFunc("/api/inbox/", srv.getLatestCode)
	mux.HandleFunc("/mail", srv.receiveMail)
	if cfg.StaticDir != "" {
		mux.Handle("/", spaFileServer(cfg.StaticDir))
	}

	log.Printf("MailOTP API listening on %s", cfg.HTTPAddr)
	if err := http.ListenAndServe(cfg.HTTPAddr, withCORS(cfg, mux)); err != nil {
		log.Fatal(err)
	}
}

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *server) getConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, configResponse{MailDomain: s.cfg.MailDomain})
}

func (s *server) createInbox(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req inboxCreateRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	alias := strings.TrimSpace(req.Alias)
	var err error
	if alias == "" {
		alias, err = s.generateUniqueAlias(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else {
		alias, err = normalizeAlias(alias)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		if existing, err := s.store.FindInbox(r.Context(), alias); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		} else if existing != nil {
			writeError(w, http.StatusConflict, "alias already exists")
			return
		}
	}

	id, err := newUUID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	inbox, err := s.store.CreateInbox(r.Context(), store.Inbox{
		ID:        id,
		UserID:    strings.TrimSpace(req.UserID),
		Alias:     alias,
		Label:     strings.TrimSpace(req.Label),
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, s.inboxResponse(*inbox, []store.Message{}))
}

func (s *server) getInbox(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	alias, err := normalizeAlias(strings.TrimPrefix(r.URL.Path, "/api/inboxes/"))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			writeError(w, http.StatusUnprocessableEntity, "limit must be 1-100")
			return
		}
		limit = parsed
	}

	inbox, err := s.store.FindInbox(r.Context(), alias)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if inbox == nil {
		writeError(w, http.StatusNotFound, "inbox not found")
		return
	}

	messages, err := s.store.Messages(r.Context(), alias, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.inboxResponse(*inbox, messages))
}

func (s *server) getLatestCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	alias, err := normalizeAlias(strings.TrimPrefix(r.URL.Path, "/api/inbox/"))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	message, err := s.store.LatestMessage(r.Context(), alias)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var code *string
	var at *time.Time
	response := latestResponse{
		Alias: alias,
		Email: s.emailFor(alias),
		Code:  code,
		Time:  at,
	}
	if message != nil {
		response.Sender = message.Sender
		response.Subject = message.Subject
		response.Code = stringPtr(message.Code)
		response.Time = &message.CreatedAt
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *server) receiveMail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+s.cfg.WebhookSecret {
		writeError(w, http.StatusUnauthorized, "invalid worker authorization")
		return
	}

	var req mailRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	rawAlias := req.Alias
	if rawAlias == "" {
		rawAlias = req.Recipient
	}
	alias, err := normalizeAlias(rawAlias)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	inboxID, err := newUUID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := s.store.EnsureInbox(r.Context(), store.Inbox{
		ID:        inboxID,
		Alias:     alias,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	code := strings.TrimSpace(req.Code)
	if code == "" {
		code = otp.Extract(req.Subject, req.Content)
	}

	messageID, err := newUUID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	message, err := s.store.SaveMessage(r.Context(), store.Message{
		ID:                messageID,
		Alias:             alias,
		Recipient:         strings.TrimSpace(req.Recipient),
		Sender:            strings.TrimSpace(req.Sender),
		Subject:           strings.TrimSpace(req.Subject),
		Code:              code,
		Content:           strings.TrimSpace(req.Content),
		ProviderMessageID: strings.TrimSpace(req.ProviderMessageID),
		CreatedAt:         time.Now().UTC(),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, message)
}

func (s *server) inboxResponse(inbox store.Inbox, messages []store.Message) inboxResponse {
	return inboxResponse{
		ID:        inbox.ID,
		UserID:    inbox.UserID,
		Alias:     inbox.Alias,
		Label:     inbox.Label,
		Email:     s.emailFor(inbox.Alias),
		CreatedAt: inbox.CreatedAt,
		Messages:  messages,
	}
}

func (s *server) generateUniqueAlias(ctx context.Context) (string, error) {
	for range 20 {
		alias, err := randomAlias(8)
		if err != nil {
			return "", err
		}
		existing, err := s.store.FindInbox(ctx, alias)
		if err != nil {
			return "", err
		}
		if existing == nil {
			return alias, nil
		}
	}
	return "", errors.New("could not generate unique alias")
}

func (s *server) emailFor(alias string) string {
	return alias + "@" + s.cfg.MailDomain
}

func normalizeAlias(value string) (string, error) {
	alias := strings.ToLower(strings.TrimSpace(value))
	if local, _, ok := strings.Cut(alias, "@"); ok {
		alias = local
	}
	if !aliasPattern.MatchString(alias) {
		return "", errors.New("invalid alias")
	}
	return alias, nil
}

func randomAlias(length int) (string, error) {
	out := make([]byte, length)
	max := big.NewInt(int64(len(aliasAlphabet)))
	for i := range out {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		out[i] = aliasAlphabet[n.Int64()]
	}
	return string(out), nil
}

func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

func readJSON(r *http.Request, target any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("write response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}

func spaFileServer(staticDir string) http.Handler {
	fileServer := http.FileServer(http.Dir(staticDir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		cleanPath := filepath.Clean(r.URL.Path)
		fullPath := filepath.Join(staticDir, cleanPath)
		if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}

		http.ServeFile(w, r, filepath.Join(staticDir, "index.html"))
	})
}

func withCORS(cfg config.Config, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && originAllowed(origin, cfg.CORSOrigins) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func originAllowed(origin string, allowed []string) bool {
	for _, candidate := range allowed {
		if candidate == "*" || candidate == origin {
			return true
		}
	}
	return false
}

func stringPtr(value string) *string {
	return &value
}

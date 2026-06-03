package store

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type Store struct {
	db       *sql.DB
	mu       sync.RWMutex
	inboxes  map[string]Inbox
	messages map[string][]Message
}

type Inbox struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id,omitempty"`
	Alias     string    `json:"alias"`
	Label     string    `json:"label,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type Message struct {
	ID                string    `json:"id"`
	Alias             string    `json:"alias"`
	Recipient         string    `json:"recipient,omitempty"`
	Sender            string    `json:"sender,omitempty"`
	Subject           string    `json:"subject,omitempty"`
	Code              string    `json:"code,omitempty"`
	Content           string    `json:"content,omitempty"`
	ProviderMessageID string    `json:"provider_message_id,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
}

const schema = `
CREATE TABLE IF NOT EXISTS inboxes (
    id UUID PRIMARY KEY,
    user_id VARCHAR(64),
    alias VARCHAR(100) NOT NULL UNIQUE,
    label VARCHAR(100),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS ix_inboxes_user_id ON inboxes(user_id);
CREATE INDEX IF NOT EXISTS ix_inboxes_label ON inboxes(label);

CREATE TABLE IF NOT EXISTS messages (
    id UUID PRIMARY KEY,
    alias VARCHAR(100) NOT NULL REFERENCES inboxes(alias) ON DELETE CASCADE,
    recipient TEXT,
    sender TEXT,
    subject TEXT,
    code VARCHAR(32),
    content TEXT,
    provider_message_id VARCHAR(255),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_messages_provider_message_id UNIQUE(provider_message_id)
);

CREATE INDEX IF NOT EXISTS ix_messages_alias ON messages(alias);
CREATE INDEX IF NOT EXISTS ix_messages_code ON messages(code);
CREATE INDEX IF NOT EXISTS ix_messages_alias_created_at ON messages(alias, created_at DESC);
`

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	if strings.EqualFold(strings.TrimSpace(databaseURL), "memory") {
		return &Store{
			inboxes:  map[string]Inbox{},
			messages: map[string][]Message{},
		}, nil
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) CreateTables(ctx context.Context) error {
	if s.db == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx, schema)
	return err
}

func (s *Store) FindInbox(ctx context.Context, alias string) (*Inbox, error) {
	if s.db == nil {
		s.mu.RLock()
		defer s.mu.RUnlock()
		inbox, ok := s.inboxes[alias]
		if !ok {
			return nil, nil
		}
		return &inbox, nil
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, COALESCE(user_id, ''), alias, COALESCE(label, ''), created_at
		FROM inboxes
		WHERE alias = $1
	`, alias)
	inbox := Inbox{}
	if err := row.Scan(&inbox.ID, &inbox.UserID, &inbox.Alias, &inbox.Label, &inbox.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &inbox, nil
}

func (s *Store) CreateInbox(ctx context.Context, inbox Inbox) (*Inbox, error) {
	if s.db == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, exists := s.inboxes[inbox.Alias]; exists {
			return nil, errors.New("alias already exists")
		}
		s.inboxes[inbox.Alias] = inbox
		created := inbox
		return &created, nil
	}

	row := s.db.QueryRowContext(ctx, `
		INSERT INTO inboxes (id, user_id, alias, label, created_at)
		VALUES ($1, NULLIF($2, ''), $3, NULLIF($4, ''), $5)
		RETURNING id, COALESCE(user_id, ''), alias, COALESCE(label, ''), created_at
	`, inbox.ID, inbox.UserID, inbox.Alias, inbox.Label, inbox.CreatedAt)
	created := Inbox{}
	if err := row.Scan(&created.ID, &created.UserID, &created.Alias, &created.Label, &created.CreatedAt); err != nil {
		return nil, err
	}
	return &created, nil
}

func (s *Store) EnsureInbox(ctx context.Context, inbox Inbox) (*Inbox, error) {
	if existing, err := s.FindInbox(ctx, inbox.Alias); err != nil || existing != nil {
		return existing, err
	}
	return s.CreateInbox(ctx, inbox)
}

func (s *Store) Messages(ctx context.Context, alias string, limit int) ([]Message, error) {
	if s.db == nil {
		s.mu.RLock()
		defer s.mu.RUnlock()
		stored := append([]Message(nil), s.messages[alias]...)
		sort.Slice(stored, func(i, j int) bool {
			return stored[i].CreatedAt.After(stored[j].CreatedAt)
		})
		if len(stored) > limit {
			stored = stored[:limit]
		}
		return stored, nil
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, alias, COALESCE(recipient, ''), COALESCE(sender, ''), COALESCE(subject, ''),
		       COALESCE(code, ''), COALESCE(content, ''), COALESCE(provider_message_id, ''), created_at
		FROM messages
		WHERE alias = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, alias, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	messages := []Message{}
	for rows.Next() {
		message := Message{}
		if err := rows.Scan(
			&message.ID,
			&message.Alias,
			&message.Recipient,
			&message.Sender,
			&message.Subject,
			&message.Code,
			&message.Content,
			&message.ProviderMessageID,
			&message.CreatedAt,
		); err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, rows.Err()
}

func (s *Store) LatestMessage(ctx context.Context, alias string) (*Message, error) {
	if s.db == nil {
		messages, err := s.Messages(ctx, alias, 1)
		if err != nil || len(messages) == 0 {
			return nil, err
		}
		return &messages[0], nil
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, alias, COALESCE(recipient, ''), COALESCE(sender, ''), COALESCE(subject, ''),
		       COALESCE(code, ''), COALESCE(content, ''), COALESCE(provider_message_id, ''), created_at
		FROM messages
		WHERE alias = $1
		ORDER BY created_at DESC
		LIMIT 1
	`, alias)
	message := Message{}
	if err := row.Scan(
		&message.ID,
		&message.Alias,
		&message.Recipient,
		&message.Sender,
		&message.Subject,
		&message.Code,
		&message.Content,
		&message.ProviderMessageID,
		&message.CreatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &message, nil
}

func (s *Store) SaveMessage(ctx context.Context, message Message) (*Message, error) {
	if s.db == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, exists := s.inboxes[message.Alias]; !exists {
			return nil, errors.New("inbox not found")
		}
		s.messages[message.Alias] = append(s.messages[message.Alias], message)
		created := message
		return &created, nil
	}

	row := s.db.QueryRowContext(ctx, `
		INSERT INTO messages (
			id, alias, recipient, sender, subject, code, content, provider_message_id, created_at
		)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''),
		        NULLIF($7, ''), NULLIF($8, ''), $9)
		RETURNING id, alias, COALESCE(recipient, ''), COALESCE(sender, ''), COALESCE(subject, ''),
		          COALESCE(code, ''), COALESCE(content, ''), COALESCE(provider_message_id, ''), created_at
	`, message.ID, message.Alias, message.Recipient, message.Sender, message.Subject, message.Code, message.Content, message.ProviderMessageID, message.CreatedAt)
	created := Message{}
	if err := row.Scan(
		&created.ID,
		&created.Alias,
		&created.Recipient,
		&created.Sender,
		&created.Subject,
		&created.Code,
		&created.Content,
		&created.ProviderMessageID,
		&created.CreatedAt,
	); err != nil {
		return nil, err
	}
	return &created, nil
}

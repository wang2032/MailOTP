package config

import (
	"bufio"
	"os"
	"strings"
)

type Config struct {
	DatabaseURL      string
	WebhookSecret    string
	MailDomain       string
	HTTPAddr         string
	StaticDir        string
	AutoCreateTables bool
	CORSOrigins      []string
}

func Load() Config {
	loadDotEnv(".env")

	return Config{
		DatabaseURL:      env("DATABASE_URL", "postgres://mailotp:mailotp@localhost:5432/mailotp?sslmode=disable"),
		WebhookSecret:    env("WEBHOOK_SECRET", "change-me"),
		MailDomain:       env("MAIL_DOMAIN", "mailotp.com"),
		HTTPAddr:         env("HTTP_ADDR", ":8000"),
		StaticDir:        env("STATIC_DIR", ""),
		AutoCreateTables: env("AUTO_CREATE_TABLES", "true") == "true",
		CORSOrigins:      splitCSV(env("CORS_ORIGINS", "http://localhost:3000")),
	}
}

func loadDotEnv(path string) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key != "" && os.Getenv(key) == "" {
			_ = os.Setenv(key, value)
		}
	}
}

func env(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

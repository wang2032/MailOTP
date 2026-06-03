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

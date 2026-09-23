CREATE TABLE telegram_link_tokens (
    token_hash BYTEA PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ
);
CREATE INDEX telegram_link_tokens_active_idx ON telegram_link_tokens (user_id, expires_at) WHERE used_at IS NULL;

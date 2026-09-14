CREATE TABLE notification_recipients (user_id UUID PRIMARY KEY, telegram_id BIGINT, is_active BOOLEAN NOT NULL, updated_at TIMESTAMPTZ NOT NULL DEFAULT now());
CREATE TABLE processed_events (event_id UUID PRIMARY KEY, processed_at TIMESTAMPTZ NOT NULL DEFAULT now());
CREATE TABLE notifications (id UUID PRIMARY KEY, event_id UUID NOT NULL, recipient_id UUID NOT NULL, channel TEXT NOT NULL CHECK(channel='TELEGRAM'), status TEXT NOT NULL CHECK(status IN ('PENDING','SENT','FAILED')), error_message TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT now(), sent_at TIMESTAMPTZ);

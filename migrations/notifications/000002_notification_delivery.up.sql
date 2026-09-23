ALTER TABLE notifications ADD COLUMN attempts INTEGER NOT NULL DEFAULT 0;
ALTER TABLE notifications ADD CONSTRAINT notifications_event_recipient_key UNIQUE (event_id, recipient_id);

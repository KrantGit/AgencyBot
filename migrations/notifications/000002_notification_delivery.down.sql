ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_event_recipient_key;
ALTER TABLE notifications DROP COLUMN IF EXISTS attempts;

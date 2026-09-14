CREATE TABLE orders (
    id UUID PRIMARY KEY, order_date TIMESTAMPTZ NOT NULL, location TEXT NOT NULL,
    amount NUMERIC(14,2) NOT NULL CHECK (amount > 0), customer_name TEXT NOT NULL,
    customer_phone TEXT NOT NULL, customer_contact TEXT NOT NULL, comment TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL CHECK (status IN ('PLANNED','COMPLETED','CANCELLED')),
    created_by UUID NOT NULL, updated_by UUID NOT NULL, version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE order_performers (order_id UUID NOT NULL REFERENCES orders(id), performer_id UUID NOT NULL, assigned_by UUID NOT NULL, assigned_at TIMESTAMPTZ NOT NULL DEFAULT now(), PRIMARY KEY(order_id, performer_id));
CREATE TABLE order_history (id UUID PRIMARY KEY, order_id UUID NOT NULL REFERENCES orders(id), actor_user_id UUID NOT NULL, action TEXT NOT NULL, changes JSONB NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now());
CREATE TABLE outbox_events (id UUID PRIMARY KEY, aggregate_id UUID NOT NULL, event_type TEXT NOT NULL, payload JSONB NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now(), published_at TIMESTAMPTZ, attempts INTEGER NOT NULL DEFAULT 0);
CREATE INDEX orders_order_date_idx ON orders(order_date);
CREATE INDEX orders_status_order_date_idx ON orders(status, order_date);
CREATE INDEX order_performers_performer_order_idx ON order_performers(performer_id, order_id);
CREATE INDEX order_history_order_created_idx ON order_history(order_id, created_at DESC);
CREATE INDEX outbox_events_unpublished_idx ON outbox_events(created_at) WHERE published_at IS NULL;

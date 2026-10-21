-- Subscription creation time: Stripe's created and start_date stay fixed
-- while current periods roll over.
ALTER TABLE subscriptions ADD COLUMN created_at TEXT NOT NULL DEFAULT '';

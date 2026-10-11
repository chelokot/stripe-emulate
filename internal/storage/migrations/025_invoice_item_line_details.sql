-- Invoice line details: the subscription item a line bills, whether it is a
-- proration, and the service period it covers (Stripe line period/parent).
ALTER TABLE invoice_items ADD COLUMN subscription_item_id TEXT NOT NULL DEFAULT '';
ALTER TABLE invoice_items ADD COLUMN proration INTEGER NOT NULL DEFAULT 0;
ALTER TABLE invoice_items ADD COLUMN period_start TEXT NOT NULL DEFAULT '';
ALTER TABLE invoice_items ADD COLUMN period_end TEXT NOT NULL DEFAULT '';

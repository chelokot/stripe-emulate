-- Payment methods minted from Stripe test tokens (pm_card_visa, ...): the
-- token keeps driving brand and payment outcome simulation for the minted id.
CREATE TABLE IF NOT EXISTS payment_methods (
	id TEXT PRIMARY KEY,
	token TEXT NOT NULL,
	created_at TEXT NOT NULL
);

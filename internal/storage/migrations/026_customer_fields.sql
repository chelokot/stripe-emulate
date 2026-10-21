-- Stripe customer fields beyond email/name, and soft deletion so a deleted
-- customer still retrieves as a deleted stub.
ALTER TABLE customers ADD COLUMN description TEXT NOT NULL DEFAULT '';
ALTER TABLE customers ADD COLUMN phone TEXT NOT NULL DEFAULT '';
ALTER TABLE customers ADD COLUMN preferred_locales TEXT NOT NULL DEFAULT '[]';
ALTER TABLE customers ADD COLUMN invoice_prefix TEXT NOT NULL DEFAULT '';
ALTER TABLE customers ADD COLUMN deleted INTEGER NOT NULL DEFAULT 0;

-- Invoice item tax_rates snapshots (POST /v1/invoiceitems tax_rates[]).
ALTER TABLE invoice_items ADD COLUMN tax_rates TEXT NOT NULL DEFAULT '[]';

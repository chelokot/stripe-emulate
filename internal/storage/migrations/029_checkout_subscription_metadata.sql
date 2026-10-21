-- subscription_data[metadata] from checkout: becomes the created subscription's metadata.
ALTER TABLE checkout_sessions ADD COLUMN subscription_metadata TEXT NOT NULL DEFAULT '{}';

ALTER TABLE jobs ADD COLUMN tier text NOT NULL DEFAULT 'local';
ALTER TABLE jobs ADD COLUMN lease_owner text;

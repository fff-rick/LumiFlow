-- FluxFeed uses email addresses as account identifiers. The legacy column
-- name stays stable so existing repositories and unique indexes keep working.
ALTER TABLE identity.users MODIFY COLUMN username VARCHAR(254) NOT NULL;

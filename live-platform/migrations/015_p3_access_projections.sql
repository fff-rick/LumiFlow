CREATE TABLE IF NOT EXISTS identity.outbox_events LIKE identity.identity_outbox_events;

INSERT IGNORE INTO identity.outbox_events
SELECT * FROM identity.identity_outbox_events;

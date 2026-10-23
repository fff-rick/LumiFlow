CREATE TABLE IF NOT EXISTS room.outbox_events LIKE room.room_outbox_events;

INSERT IGNORE INTO room.outbox_events
SELECT * FROM room.room_outbox_events;

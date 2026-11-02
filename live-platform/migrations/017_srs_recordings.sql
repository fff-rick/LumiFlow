CREATE TABLE IF NOT EXISTS room.live_recordings (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    room_id BIGINT NOT NULL,
    provider VARCHAR(32) NOT NULL,
    object_key VARCHAR(512) NOT NULL,
    created_at DATETIME(3) NOT NULL,
    UNIQUE KEY uk_live_recording_object (object_key),
    KEY idx_live_recordings_room_created (room_id, created_at),
    CONSTRAINT fk_live_recording_room FOREIGN KEY (room_id) REFERENCES room.live_rooms(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

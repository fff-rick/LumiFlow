CREATE TABLE IF NOT EXISTS room.live_media_sessions (
    room_id BIGINT NOT NULL PRIMARY KEY,
    provider VARCHAR(32) NOT NULL,
    app VARCHAR(64) NOT NULL,
    stream_name VARCHAR(128) NOT NULL,
    publish_token_hash BINARY(32) NOT NULL,
    status VARCHAR(24) NOT NULL,
    published_at DATETIME(3) NULL,
    disconnected_at DATETIME(3) NULL,
    created_at DATETIME(3) NOT NULL,
    updated_at DATETIME(3) NOT NULL,
    UNIQUE KEY uk_live_media_stream (app, stream_name),
    CONSTRAINT fk_live_media_room FOREIGN KEY (room_id) REFERENCES room.live_rooms(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

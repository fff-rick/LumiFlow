ALTER TABLE room.live_recordings
    ADD COLUMN storage_status VARCHAR(16) NOT NULL DEFAULT 'LOCAL' AFTER object_key,
    ADD COLUMN size_bytes BIGINT NOT NULL DEFAULT 0 AFTER storage_status,
    ADD COLUMN archive_attempts INT NOT NULL DEFAULT 0 AFTER size_bytes,
    ADD COLUMN archive_started_at DATETIME(3) NULL AFTER archive_attempts,
    ADD COLUMN next_attempt_at DATETIME(3) NULL AFTER archive_started_at,
    ADD COLUMN archived_at DATETIME(3) NULL AFTER next_attempt_at,
    ADD COLUMN last_error VARCHAR(512) NULL AFTER archived_at,
    ADD COLUMN updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) AFTER last_error,
    ADD KEY idx_live_recordings_archive (storage_status, next_attempt_at, id);

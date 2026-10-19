CREATE DATABASE IF NOT EXISTS video CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS video.videos (
    id BIGINT NOT NULL AUTO_INCREMENT,
    author_id BIGINT NOT NULL,
    title VARCHAR(128) NOT NULL,
    description VARCHAR(512) NOT NULL DEFAULT '',
    media_url VARCHAR(512) NOT NULL,
    cover_url VARCHAR(512) NOT NULL,
    status TINYINT NOT NULL DEFAULT 2,
    published_at DATETIME(3) NULL,
    idempotency_key VARCHAR(128) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY ux_video_author_idempotency (author_id, idempotency_key),
    KEY idx_video_author_status_published (author_id, status, published_at, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS video.video_stats (
    video_id BIGINT NOT NULL,
    like_count BIGINT NOT NULL DEFAULT 0,
    comment_count BIGINT NOT NULL DEFAULT 0,
    favorite_count BIGINT NOT NULL DEFAULT 0,
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (video_id),
    CONSTRAINT fk_video_stats_video FOREIGN KEY (video_id) REFERENCES video.videos(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

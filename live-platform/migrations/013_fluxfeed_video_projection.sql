CREATE TABLE IF NOT EXISTS query.fluxfeed_video_projection (
    flux_video_id BIGINT PRIMARY KEY,
    fluxfeed_user_id BIGINT NOT NULL,
    title VARCHAR(255) NOT NULL,
    description TEXT NOT NULL,
    media_url VARCHAR(1024) NOT NULL,
    cover_url VARCHAR(1024) NOT NULL,
    visible TINYINT NOT NULL,
    published_at DATETIME(3) NOT NULL,
    updated_at DATETIME(3) NOT NULL,
    KEY idx_fluxfeed_video_visible_published(visible, published_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

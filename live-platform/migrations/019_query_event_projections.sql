DROP VIEW IF EXISTS query.room_message_feed;
DROP VIEW IF EXISTS query.user_display_projection;

CREATE TABLE query.user_display_projection (
    user_id BIGINT PRIMARY KEY,
    nickname VARCHAR(64) NOT NULL,
    avatar VARCHAR(128) NOT NULL DEFAULT '',
    status TINYINT NOT NULL,
    aggregate_version BIGINT NOT NULL DEFAULT 0,
    projected_at DATETIME(3) NOT NULL,
    created_at DATETIME(3) NOT NULL,
    updated_at DATETIME(3) NOT NULL,
    KEY idx_query_user_status(status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE query.room_message_feed (
    sequence BIGINT PRIMARY KEY AUTO_INCREMENT,
    type VARCHAR(16) NOT NULL,
    message_id VARCHAR(72) NOT NULL,
    room_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    nickname VARCHAR(64) NOT NULL DEFAULT '',
    content VARCHAR(512) NOT NULL DEFAULT '',
    gift_name VARCHAR(64) NOT NULL DEFAULT '',
    gift_count BIGINT NOT NULL DEFAULT 0,
    created_at DATETIME(3) NOT NULL,
    UNIQUE KEY uk_query_message_id(message_id),
    KEY idx_query_room_created(room_id, created_at, sequence)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

INSERT INTO query.user_display_projection(user_id, nickname, avatar, status, aggregate_version, projected_at, created_at, updated_at)
SELECT u.id, u.nickname, u.avatar, u.status,
       COALESCE((SELECT MAX(o.id) FROM identity.outbox_events o WHERE o.aggregate_type='IDENTITY' AND o.aggregate_id=CAST(u.id AS CHAR)), 0),
       u.updated_at, u.created_at, u.updated_at
FROM identity.users u;

INSERT IGNORE INTO query.room_message_feed(type, message_id, room_id, user_id, nickname, content, gift_name, gift_count, created_at)
SELECT 'danmaku', message_id, room_id, user_id, nickname, content, '', 0, created_at
FROM archive.danmaku_records;

INSERT IGNORE INTO query.room_message_feed(type, message_id, room_id, user_id, nickname, content, gift_name, gift_count, created_at)
SELECT 'gift', CONCAT('gift:', o.order_no), o.room_id, o.user_id, '', '', g.name, o.gift_count, o.created_at
FROM commerce.gift_orders o
JOIN commerce.gifts g ON g.id = o.gift_id
WHERE o.status = 1;

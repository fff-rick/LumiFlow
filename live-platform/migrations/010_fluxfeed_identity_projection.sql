CREATE TABLE IF NOT EXISTS identity.external_identities (
    provider VARCHAR(32) NOT NULL,
    subject VARCHAR(255) NOT NULL,
    user_id BIGINT NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (provider, subject),
    UNIQUE KEY ux_external_identities_user_provider (user_id, provider)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

ALTER TABLE identity.users MODIFY COLUMN avatar VARCHAR(512) NOT NULL DEFAULT '/front_con/ava/布偶.svg';

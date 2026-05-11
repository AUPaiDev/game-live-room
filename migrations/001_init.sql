-- migrations/001_init.sql
-- Initial schema for game-live-room
-- This mirrors the GORM AutoMigrate models in internal/model/model.go

CREATE TABLE IF NOT EXISTS `live_events` (
  `id`          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `cmd`         VARCHAR(64)     NOT NULL,
  `uid`         BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `username`    VARCHAR(128)    NOT NULL DEFAULT '',
  `text`        TEXT,
  `gift_name`   VARCHAR(128)    NOT NULL DEFAULT '',
  `gift_count`  INT             NOT NULL DEFAULT 0,
  `price`       INT             NOT NULL DEFAULT 0,
  `guard_level` INT             NOT NULL DEFAULT 0,
  `created_at`  DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  INDEX `idx_cmd`        (`cmd`),
  INDEX `idx_uid`        (`uid`),
  INDEX `idx_created_at` (`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `quiz_questions` (
  `id`          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `type`        VARCHAR(16)     NOT NULL,
  `question`    TEXT            NOT NULL,
  `options`     JSON,
  `answer`      INT             NOT NULL DEFAULT 0,
  `media_path`  VARCHAR(512)    NOT NULL DEFAULT '',
  `explanation` TEXT,
  `created_at`  DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at`  DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `quiz_sessions` (
  `id`            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `question_id`   BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `started_at`    DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `ended_at`      DATETIME(3),
  `total_answers` INT             NOT NULL DEFAULT 0,
  `correct_count` INT             NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  INDEX `idx_question_id` (`question_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `vote_records` (
  `id`         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `session_id` VARCHAR(64)     NOT NULL,
  `slot_id`    VARCHAR(64)     NOT NULL,
  `uid`        BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `username`   VARCHAR(128)    NOT NULL DEFAULT '',
  `gift_name`  VARCHAR(128)    NOT NULL DEFAULT '',
  `gift_count` INT             NOT NULL DEFAULT 0,
  `score`      INT             NOT NULL DEFAULT 0,
  `created_at` DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  INDEX `idx_session_id` (`session_id`),
  INDEX `idx_slot_id`    (`slot_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `vote_sessions` (
  `id`         VARCHAR(64)  NOT NULL,
  `status`     VARCHAR(16)  NOT NULL DEFAULT 'active',
  `config`     JSON,
  `started_at` DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `ended_at`   DATETIME(3),
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

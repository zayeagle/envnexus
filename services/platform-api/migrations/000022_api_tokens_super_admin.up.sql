ALTER TABLE `api_tokens` ADD COLUMN `is_super_admin` tinyint(1) NOT NULL DEFAULT 0 AFTER `scopes`;

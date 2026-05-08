CREATE TABLE `api_tokens` (
  `id` varchar(26) NOT NULL,
  `user_id` varchar(26) NOT NULL,
  `tenant_id` varchar(26) NOT NULL,
  `name` varchar(255) NOT NULL,
  `token_hash` varchar(64) NOT NULL,
  `token_prefix` varchar(12) NOT NULL,
  `scopes` json DEFAULT NULL,
  `expires_at` datetime(3) DEFAULT NULL,
  `last_used_at` datetime(3) DEFAULT NULL,
  `revoked_at` datetime(3) DEFAULT NULL,
  `created_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `ux_api_tokens_token_hash` (`token_hash`),
  KEY `idx_api_tokens_user_id` (`user_id`),
  KEY `idx_api_tokens_tenant_id` (`tenant_id`),
  KEY `idx_api_tokens_token_prefix` (`token_prefix`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

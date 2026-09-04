-- Applied on every Open. Auth ships with the template, so these tables always
-- exist; scaffolded application tables are created separately via execute_sql.

CREATE TABLE IF NOT EXISTS users (
    id            INTEGER  PRIMARY KEY,
    name          TEXT     NOT NULL,
    email         TEXT     NOT NULL UNIQUE,
    password_hash TEXT     NOT NULL,
    -- Bumped to revoke every session issued before the bump. A cookie carries
    -- the epoch it was minted under and is rejected once it falls behind.
    session_epoch INTEGER  NOT NULL DEFAULT 0,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- One row per rate-limit bucket. The key is namespaced by action, so a success
-- on one endpoint cannot clear another's failures. Times are unix seconds so
-- every comparison is numeric.
CREATE TABLE IF NOT EXISTS rate_limits (
    bucket       TEXT     PRIMARY KEY,
    attempts     INTEGER  NOT NULL DEFAULT 0,
    locked_until INTEGER,
    updated_at   INTEGER  NOT NULL
);

-- Bearer tokens for native clients. Only the SHA-256 hash is stored.
-- expires_at is a unix timestamp so comparisons are numeric.
CREATE TABLE IF NOT EXISTS mobile_tokens (
    token_hash TEXT     PRIMARY KEY,
    user_id    INTEGER  NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at INTEGER  NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_mobile_tokens_user ON mobile_tokens(user_id);

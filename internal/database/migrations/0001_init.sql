-- confmcp initial schema.

CREATE TABLE IF NOT EXISTS users (
    id                BIGSERIAL PRIMARY KEY,
    username          VARCHAR(255) NOT NULL UNIQUE,
    email             VARCHAR(320),
    display_name      VARCHAR(255),
    password_hash     TEXT,
    keycloak_sub      VARCHAR(255) UNIQUE,
    keycloak_issuer   VARCHAR(512),
    is_service_admin  BOOLEAN NOT NULL DEFAULT FALSE,
    roles             TEXT[] NOT NULL DEFAULT '{}',
    active            BOOLEAN NOT NULL DEFAULT TRUE,
    source            VARCHAR(20) NOT NULL DEFAULT 'local',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_login_at     TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS web_sessions (
    id          UUID PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL UNIQUE,
    ip          VARCHAR(64),
    user_agent  TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ NOT NULL,
    revoked_at  TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS web_sessions_user_idx ON web_sessions(user_id);

-- Runtime settings. Secret values are stored AES-256-GCM sealed inside value_json.
CREATE TABLE IF NOT EXISTS settings (
    key         VARCHAR(128) PRIMARY KEY,
    value_json  JSONB,
    value_enc   TEXT,
    is_secret   BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by  VARCHAR(255)
);

-- Durable identity: (issuer, sub) <-> (instance, userKey). The username is
-- kept for display and for re-verification only.
CREATE TABLE IF NOT EXISTS confluence_identity_mapping (
    id                  BIGSERIAL PRIMARY KEY,
    keycloak_issuer     VARCHAR(512) NOT NULL DEFAULT '',
    keycloak_sub        VARCHAR(255) NOT NULL UNIQUE,
    keycloak_username   VARCHAR(255) NOT NULL,
    instance_id         VARCHAR(64) NOT NULL DEFAULT 'default',
    confluence_user_key VARCHAR(255) NOT NULL,
    confluence_username VARCHAR(255) NOT NULL,
    confluence_display  VARCHAR(255),
    confluence_email    VARCHAR(320),
    mapping_type        VARCHAR(20) NOT NULL DEFAULT 'auto',
    evidence            VARCHAR(64) NOT NULL DEFAULT '',
    active              BOOLEAN NOT NULL DEFAULT TRUE,
    last_error          TEXT,
    mapped_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    verified_at         TIMESTAMPTZ,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (instance_id, confluence_user_key)
);

CREATE TABLE IF NOT EXISTS identity_mapping_history (
    id                  BIGSERIAL PRIMARY KEY,
    keycloak_sub        VARCHAR(255) NOT NULL,
    action              VARCHAR(32) NOT NULL,
    confluence_user_key VARCHAR(255),
    confluence_username VARCHAR(255),
    actor               VARCHAR(255),
    note                TEXT,
    occurred_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS identity_history_sub_idx ON identity_mapping_history(keycloak_sub, occurred_at DESC);

-- One row per subject that failed to map; repeats bump the counter.
CREATE TABLE IF NOT EXISTS identity_mapping_errors (
    id                BIGSERIAL PRIMARY KEY,
    keycloak_sub      VARCHAR(255) NOT NULL UNIQUE,
    keycloak_username VARCHAR(255) NOT NULL,
    reason            TEXT NOT NULL,
    occurrences       INT NOT NULL DEFAULT 1,
    first_seen_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    occurred_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Optional delegated credential (user Basic) for the delegated REST mode.
CREATE TABLE IF NOT EXISTS user_confluence_credential (
    user_id             BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    confluence_username VARCHAR(255) NOT NULL,
    confluence_user_key VARCHAR(255),
    secret_enc          TEXT NOT NULL,
    verified_at         TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Key permission scheme: editable roles granting scopes to personal API keys.
CREATE TABLE IF NOT EXISTS key_roles (
    name        VARCHAR(64) PRIMARY KEY,
    description TEXT NOT NULL DEFAULT '',
    scopes      TEXT[] NOT NULL DEFAULT '{}',
    builtin     BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS api_keys (
    id             UUID PRIMARY KEY,
    user_id        BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name           VARCHAR(255) NOT NULL,
    prefix         VARCHAR(32) NOT NULL UNIQUE,
    key_hmac       TEXT NOT NULL,
    key_role       VARCHAR(64) REFERENCES key_roles(name) ON DELETE SET NULL,
    scopes         TEXT[] NOT NULL DEFAULT '{}',
    rotated_from   UUID,
    rotation_due_at TIMESTAMPTZ,
    expires_at     TIMESTAMPTZ,
    last_used_at   TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at     TIMESTAMPTZ,
    revoked_reason TEXT
);
CREATE INDEX IF NOT EXISTS api_keys_user_idx ON api_keys(user_id);

-- MCP tool registry. Rows are reconciled from code on boot; admin edits persist.
CREATE TABLE IF NOT EXISTS mcp_tools (
    name              VARCHAR(128) PRIMARY KEY,
    title             VARCHAR(255) NOT NULL DEFAULT '',
    description       TEXT NOT NULL DEFAULT '',
    tool_group        VARCHAR(64) NOT NULL DEFAULT 'general',
    risk_level        VARCHAR(16) NOT NULL DEFAULT 'READ',
    required_perm     VARCHAR(32) NOT NULL DEFAULT 'read',
    priority          VARCHAR(4) NOT NULL DEFAULT 'P0',
    enabled           BOOLEAN NOT NULL DEFAULT TRUE,
    requires_approval BOOLEAN NOT NULL DEFAULT FALSE,
    min_role          VARCHAR(64) NOT NULL DEFAULT 'confluence-mcp-reader',
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Access policy over spaces, page trees and content types. Deny wins.
CREATE TABLE IF NOT EXISTS policy_rules (
    id          BIGSERIAL PRIMARY KEY,
    kind        VARCHAR(16) NOT NULL,
    pattern     VARCHAR(255) NOT NULL,
    space_key   VARCHAR(255) NOT NULL DEFAULT '',
    effect      VARCHAR(8) NOT NULL,
    risk_cap    VARCHAR(16),
    priority    INT NOT NULL DEFAULT 100,
    note        TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Policy generation: bumped on every rule change, so approvals and search
-- cursors granted under an older policy go stale.
CREATE TABLE IF NOT EXISTS policy_state (
    id          INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    generation  BIGINT NOT NULL DEFAULT 1,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO policy_state(id, generation) VALUES (1, 1) ON CONFLICT (id) DO NOTHING;

-- Approval requests bind to the requester, tool, normalised argument hash,
-- policy generation, target version and target content hash.
CREATE TABLE IF NOT EXISTS approval_requests (
    id                UUID PRIMARY KEY,
    keycloak_sub      VARCHAR(255) NOT NULL,
    user_id           BIGINT REFERENCES users(id) ON DELETE SET NULL,
    username          VARCHAR(255) NOT NULL,
    instance_id       VARCHAR(64) NOT NULL DEFAULT 'default',
    tool_name         VARCHAR(128) NOT NULL,
    risk_level        VARCHAR(16) NOT NULL DEFAULT 'WRITE',
    arguments_hash    VARCHAR(64) NOT NULL,
    arguments_redacted JSONB,
    resource          VARCHAR(512) NOT NULL DEFAULT '',
    space_key         VARCHAR(255) NOT NULL DEFAULT '',
    target_id         VARCHAR(64) NOT NULL DEFAULT '',
    target_version    INT,
    target_hash       VARCHAR(64) NOT NULL DEFAULT '',
    policy_generation BIGINT NOT NULL DEFAULT 0,
    preview_enc       TEXT,
    requires_approver BOOLEAN NOT NULL DEFAULT FALSE,
    status            VARCHAR(20) NOT NULL,
    decided_by        VARCHAR(255),
    decision_note     TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at        TIMESTAMPTZ NOT NULL,
    approved_at       TIMESTAMPTZ,
    consumed_at       TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS approval_status_idx ON approval_requests(status, expires_at);
CREATE INDEX IF NOT EXISTS approval_sub_idx ON approval_requests(keycloak_sub, created_at DESC);

-- Write executions. The idempotency key ties a requester, tool and argument
-- hash together so the same approved write is never sent twice.
CREATE TABLE IF NOT EXISTS operation_records (
    id               UUID PRIMARY KEY,
    idempotency_key  VARCHAR(64) NOT NULL UNIQUE,
    keycloak_sub     VARCHAR(255) NOT NULL,
    username         VARCHAR(255) NOT NULL,
    tool_name        VARCHAR(128) NOT NULL,
    arguments_hash   VARCHAR(64) NOT NULL,
    approval_id      UUID,
    target_id        VARCHAR(64) NOT NULL DEFAULT '',
    status           VARCHAR(20) NOT NULL,
    upstream_id      VARCHAR(64),
    result_version   INT,
    error_code       VARCHAR(64),
    message          TEXT,
    executed_by      VARCHAR(255),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_by      VARCHAR(255),
    resolved_at      TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS operation_status_idx ON operation_records(status, created_at DESC);

-- Files uploaded ahead of confluence_upload_attachment. Kept in the database
-- so an air-gapped single-container install needs no shared volume.
CREATE TABLE IF NOT EXISTS attachment_uploads (
    id          UUID PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    filename    VARCHAR(255) NOT NULL,
    media_type  VARCHAR(255) NOT NULL,
    size_bytes  BIGINT NOT NULL,
    sha256      VARCHAR(64) NOT NULL,
    data        BYTEA NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS attachment_uploads_user_idx ON attachment_uploads(user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS audit_log (
    id                  BIGSERIAL PRIMARY KEY,
    occurred_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    category            VARCHAR(32) NOT NULL,
    action              VARCHAR(128) NOT NULL,
    request_id          VARCHAR(64),
    keycloak_sub        VARCHAR(255),
    keycloak_username   VARCHAR(255),
    confluence_user_key VARCHAR(255),
    confluence_username VARCHAR(255),
    executed_as         VARCHAR(255),
    mcp_client          VARCHAR(255),
    auth_mode           VARCHAR(32),
    tool_name           VARCHAR(128),
    space_key           VARCHAR(255),
    content_id          VARCHAR(64),
    content_version     INT,
    decision            VARCHAR(32),
    approval_id         UUID,
    success             BOOLEAN NOT NULL DEFAULT TRUE,
    error_code          VARCHAR(64),
    message             TEXT,
    latency_ms          INT,
    upstream_ms         INT,
    ip                  VARCHAR(64),
    detail              JSONB
);
CREATE INDEX IF NOT EXISTS audit_time_idx ON audit_log(occurred_at DESC);
CREATE INDEX IF NOT EXISTS audit_cat_idx ON audit_log(category, occurred_at DESC);
CREATE INDEX IF NOT EXISTS audit_user_idx ON audit_log(keycloak_username, occurred_at DESC);
CREATE INDEX IF NOT EXISTS audit_tool_idx ON audit_log(tool_name, occurred_at DESC);

CREATE TABLE IF NOT EXISTS mcp_sessions (
    id           UUID PRIMARY KEY,
    user_id      BIGINT REFERENCES users(id) ON DELETE CASCADE,
    username     VARCHAR(255) NOT NULL,
    client_name  VARCHAR(255) NOT NULL DEFAULT '',
    auth_mode    VARCHAR(32) NOT NULL DEFAULT 'oauth',
    ip           VARCHAR(64),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    closed_at    TIMESTAMPTZ
);

-- Per-user preferences for the personal (non-admin) pages.
CREATE TABLE IF NOT EXISTS user_prefs (
    user_id         BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    prefer_delegated BOOLEAN NOT NULL DEFAULT FALSE,
    theme           VARCHAR(16) NOT NULL DEFAULT 'system',
    font_scale      NUMERIC(3,2) NOT NULL DEFAULT 1.00,
    locale          VARCHAR(8) NOT NULL DEFAULT 'ko',
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- MCP OAuth: confmcp is the authorization server for MCP clients. Codes and
-- tokens are stored only as SHA-256 hashes.
CREATE TABLE IF NOT EXISTS oauth_clients (
    client_id     VARCHAR(64) PRIMARY KEY,
    client_name   VARCHAR(255) NOT NULL,
    redirect_uris TEXT[] NOT NULL,
    software_id   VARCHAR(255),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at  TIMESTAMPTZ,
    disabled_at   TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS oauth_codes (
    code_hash      VARCHAR(64) PRIMARY KEY,
    client_id      VARCHAR(64) NOT NULL REFERENCES oauth_clients(client_id) ON DELETE CASCADE,
    user_id        BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    redirect_uri   TEXT NOT NULL,
    code_challenge VARCHAR(128) NOT NULL,
    scopes         TEXT[] NOT NULL DEFAULT '{}',
    resource       TEXT NOT NULL DEFAULT '',
    expires_at     TIMESTAMPTZ NOT NULL,
    used_at        TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS oauth_grants (
    id           UUID PRIMARY KEY,
    client_id    VARCHAR(64) NOT NULL REFERENCES oauth_clients(client_id) ON DELETE CASCADE,
    user_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    scopes       TEXT[] NOT NULL DEFAULT '{}',
    resource     TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS oauth_grants_user_idx ON oauth_grants(user_id);

CREATE TABLE IF NOT EXISTS oauth_tokens (
    token_hash  VARCHAR(64) PRIMARY KEY,
    grant_id    UUID NOT NULL REFERENCES oauth_grants(id) ON DELETE CASCADE,
    kind        VARCHAR(16) NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    used_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS oauth_tokens_grant_idx ON oauth_tokens(grant_id);

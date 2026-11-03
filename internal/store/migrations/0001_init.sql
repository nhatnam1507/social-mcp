CREATE TABLE meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE credentials (
    id                 TEXT PRIMARY KEY,
    platform           TEXT NOT NULL,
    subject            TEXT NOT NULL,
    sealed_token       BLOB NOT NULL,
    expires_at         INTEGER,
    refresh_expires_at INTEGER,
    refreshable        INTEGER NOT NULL DEFAULT 0,
    status             TEXT NOT NULL DEFAULT 'ok',
    version            INTEGER NOT NULL DEFAULT 1,
    created_at         INTEGER NOT NULL,
    updated_at         INTEGER NOT NULL,
    UNIQUE (platform, subject)
);

CREATE TABLE accounts (
    id                      TEXT PRIMARY KEY,
    platform                TEXT NOT NULL,
    credential_id           TEXT NOT NULL REFERENCES credentials(id),
    external_id             TEXT NOT NULL,
    display_name            TEXT NOT NULL,
    sealed_account_token    BLOB,
    account_token_expires_at INTEGER,
    status                  TEXT NOT NULL DEFAULT 'ok',
    meta                    TEXT NOT NULL DEFAULT '{}',
    created_at              INTEGER NOT NULL,
    updated_at              INTEGER NOT NULL,
    UNIQUE (platform, external_id)
);

CREATE TABLE uploads (
    id               TEXT PRIMARY KEY,
    account_id       TEXT NOT NULL,
    platform         TEXT NOT NULL,
    file_path        TEXT NOT NULL,
    file_size        INTEGER NOT NULL,
    file_mtime       INTEGER NOT NULL DEFAULT 0,
    file_fingerprint TEXT NOT NULL DEFAULT '',
    container        TEXT NOT NULL,
    request          TEXT NOT NULL DEFAULT '{}',
    status           TEXT NOT NULL,
    phase            TEXT NOT NULL,
    bytes_sent       INTEGER NOT NULL DEFAULT 0,
    remote_id        TEXT NOT NULL DEFAULT '',
    remote_url       TEXT NOT NULL DEFAULT '',
    outcome          TEXT NOT NULL DEFAULT '',
    note             TEXT NOT NULL DEFAULT '',
    error            TEXT NOT NULL DEFAULT '',
    resume_state     TEXT NOT NULL DEFAULT '',
    attempts         INTEGER NOT NULL DEFAULT 0,
    not_before       INTEGER,
    created_at       INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL
);

CREATE INDEX uploads_status ON uploads (status, created_at);
CREATE INDEX uploads_account ON uploads (account_id, status);

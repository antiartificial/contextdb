-- contextdb schema v1

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS nodes (
    id          UUID        NOT NULL,
    namespace   TEXT        NOT NULL,
    labels      TEXT[]      NOT NULL DEFAULT '{}',
    properties  JSONB       NOT NULL DEFAULT '{}',
    vector      BYTEA,          -- stored as raw float32 bytes; pgvector used in vector_entries
    model_id    TEXT        NOT NULL DEFAULT '',
    valid_from  TIMESTAMPTZ NOT NULL,
    valid_until TIMESTAMPTZ,
    tx_time     TIMESTAMPTZ NOT NULL DEFAULT now(),
    confidence  DOUBLE PRECISION NOT NULL DEFAULT 0.5,
    version     BIGINT      NOT NULL DEFAULT 1,
    PRIMARY KEY (namespace, id, version)
);

CREATE INDEX IF NOT EXISTS idx_nodes_ns_id      ON nodes (namespace, id);
CREATE INDEX IF NOT EXISTS idx_nodes_ns_valid    ON nodes (namespace, valid_from, valid_until);

CREATE TABLE IF NOT EXISTS edges (
    id              UUID        PRIMARY KEY,
    namespace       TEXT        NOT NULL,
    src             UUID        NOT NULL,
    dst             UUID        NOT NULL,
    type            TEXT        NOT NULL,
    weight          DOUBLE PRECISION NOT NULL DEFAULT 1.0,
    properties      JSONB       NOT NULL DEFAULT '{}',
    valid_from      TIMESTAMPTZ NOT NULL,
    valid_until     TIMESTAMPTZ,
    tx_time         TIMESTAMPTZ NOT NULL DEFAULT now(),
    invalidated_at  TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_edges_src  ON edges (namespace, src)  WHERE invalidated_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_edges_dst  ON edges (namespace, dst)  WHERE invalidated_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_edges_type ON edges (namespace, type);

CREATE TABLE IF NOT EXISTS sources (
    id                  UUID        PRIMARY KEY,
    namespace           TEXT        NOT NULL,
    external_id         TEXT        NOT NULL,
    labels              TEXT[]      NOT NULL DEFAULT '{}',
    credibility_score   DOUBLE PRECISION NOT NULL DEFAULT 0.5,
    claims_asserted     BIGINT      NOT NULL DEFAULT 0,
    claims_validated    BIGINT      NOT NULL DEFAULT 0,
    claims_refuted      BIGINT      NOT NULL DEFAULT 0,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (namespace, external_id)
);

CREATE TABLE IF NOT EXISTS vector_entries (
    id          UUID        PRIMARY KEY,
    namespace   TEXT        NOT NULL,
    node_id     UUID,
    vector      BYTEA       NOT NULL,   -- raw float32 bytes for brute-force; or use vector type
    text        TEXT        NOT NULL DEFAULT '',
    model_id    TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_vector_entries_ns ON vector_entries (namespace);

CREATE TABLE IF NOT EXISTS events (
    id          UUID        PRIMARY KEY,
    namespace   TEXT        NOT NULL,
    type        TEXT        NOT NULL,
    payload     JSONB       NOT NULL DEFAULT '{}',
    tx_time     TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed   BOOLEAN     NOT NULL DEFAULT FALSE
);

CREATE INDEX IF NOT EXISTS idx_events_ns_time ON events (namespace, tx_time) WHERE NOT processed;

CREATE TABLE IF NOT EXISTS kv_store (
    key         TEXT        PRIMARY KEY,
    value       BYTEA       NOT NULL,
    expires_at  TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS schema_migrations (
    version     INT         PRIMARY KEY,
    applied_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

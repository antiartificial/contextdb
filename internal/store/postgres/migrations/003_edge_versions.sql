-- Preserve every edge edit so a KnownAt walk can resolve the edge version
-- that existed at that transaction-time cutoff.
CREATE TABLE IF NOT EXISTS edge_versions (
    id              UUID NOT NULL,
    namespace       TEXT NOT NULL,
    version         BIGINT NOT NULL,
    src             UUID NOT NULL,
    dst             UUID NOT NULL,
    type            TEXT NOT NULL,
    weight          DOUBLE PRECISION NOT NULL,
    properties      JSONB NOT NULL,
    valid_from      TIMESTAMPTZ NOT NULL,
    valid_until     TIMESTAMPTZ,
    tx_time         TIMESTAMPTZ NOT NULL,
    invalidated_at  TIMESTAMPTZ,
    PRIMARY KEY (namespace, id, version)
);

INSERT INTO edge_versions (id, namespace, version, src, dst, type, weight, properties,
                           valid_from, valid_until, tx_time, invalidated_at)
SELECT id, namespace, 1, src, dst, type, weight, properties,
       valid_from, valid_until, tx_time, invalidated_at FROM edges
ON CONFLICT DO NOTHING;

CREATE INDEX IF NOT EXISTS idx_edge_versions_src
    ON edge_versions (namespace, src, id, version DESC);

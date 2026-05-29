-- Add content fingerprints for write-time deduplication.

ALTER TABLE nodes
    ADD COLUMN IF NOT EXISTS fingerprint TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_nodes_ns_fingerprint
    ON nodes (namespace, fingerprint)
    WHERE fingerprint <> '';

ALTER TABLE sources
    ADD COLUMN IF NOT EXISTS alpha DOUBLE PRECISION NOT NULL DEFAULT 1.0,
    ADD COLUMN IF NOT EXISTS beta DOUBLE PRECISION NOT NULL DEFAULT 1.0;

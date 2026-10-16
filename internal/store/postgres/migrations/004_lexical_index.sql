-- Index all node versions. A historical query can match text that was later
-- revised, so candidate selection must not be limited to current versions.
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE INDEX IF NOT EXISTS idx_nodes_lexical_trgm
    ON nodes USING gin ((lower(CASE
        WHEN jsonb_typeof(properties->'text') = 'string' THEN properties->>'text'
        WHEN jsonb_typeof(properties->'content') = 'string' THEN properties->>'content'
        ELSE '' END)) gin_trgm_ops);

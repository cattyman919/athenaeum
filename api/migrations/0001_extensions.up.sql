CREATE EXTENSION IF NOT EXISTS vector;      -- pgvector (pg16 image ships it)
CREATE EXTENSION IF NOT EXISTS pg_trgm;     -- title similarity fallback
CREATE EXTENSION IF NOT EXISTS citext;      -- case-insensitive unique emails

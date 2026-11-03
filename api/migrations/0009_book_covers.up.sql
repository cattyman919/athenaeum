-- Migration 0009: book_covers — cover image bytes cached in Postgres (docs/02 §book_covers).
-- Keyed by the upstream URL (books.cover_url) rather than book_id so the cache survives
-- `seed --reset` (new book ids) and a changed cover_url is simply a cache miss.

CREATE TABLE book_covers (
  source_url   TEXT        PRIMARY KEY,                -- = books.cover_url the bytes came from
  content_type TEXT        NOT NULL CHECK (content_type LIKE 'image/%'),
  bytes        BYTEA       NOT NULL CHECK (octet_length(bytes) BETWEEN 1 AND 2097152),
  sha256       TEXT        NOT NULL,                    -- hex digest: ETag + ?v= cache-buster
  fetched_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

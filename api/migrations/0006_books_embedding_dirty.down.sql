DROP INDEX IF EXISTS books_embedding_dirty_idx;
ALTER TABLE books DROP COLUMN IF EXISTS embedding_dirty;

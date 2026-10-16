-- Auto-index worker backlog (docs/02 §4 books, docs/06 §2).
ALTER TABLE books ADD COLUMN embedding_dirty BOOL NOT NULL DEFAULT false;
CREATE INDEX books_embedding_dirty_idx ON books (embedding_dirty) WHERE embedding_dirty;

-- docs/02-data-model.md §5 (full-text + trigram + lookup indexes; HNSW lives in 0002).
CREATE INDEX books_fts_idx ON books
  USING gin (to_tsvector('english', title || ' ' || title || ' ' || coalesce(description, '')));
CREATE INDEX books_title_trgm_idx ON books USING gin (title gin_trgm_ops);
CREATE INDEX books_section_idx ON books (section_id);
CREATE INDEX users_role_idx ON users (role);

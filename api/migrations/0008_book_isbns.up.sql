-- Migration 0008: book_isbns table — one book, many edition ISBNs (M21, docs/02 §book_isbns).
-- books.isbn13 stays as primary/display ISBN (denormalized for back-compat);
-- every ISBN→book resolution routes through book_isbns.

CREATE TABLE book_isbns (
  isbn13     TEXT        PRIMARY KEY,
  book_id    UUID        NOT NULL REFERENCES books(id) ON DELETE CASCADE,
  is_primary BOOLEAN     NOT NULL DEFAULT false,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- At most one primary ISBN per book.
CREATE UNIQUE INDEX book_isbns_primary_key ON book_isbns (book_id) WHERE is_primary;

-- Fast reverse lookup (book_id → all its ISBNs).
CREATE INDEX book_isbns_book_id_idx ON book_isbns (book_id);

-- Backfill: one primary row per book that already has an isbn13.
INSERT INTO book_isbns (isbn13, book_id, is_primary)
SELECT isbn13, id, true
FROM   books
WHERE  isbn13 IS NOT NULL
ON CONFLICT DO NOTHING;

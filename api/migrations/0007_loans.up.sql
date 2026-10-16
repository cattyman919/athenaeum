-- Migration 0007: add loans table; drop book_copies.due_at (M17, docs/02 §loans).
-- book_copies.due_at was a "display only" column (docs/10); the loans table is now the
-- authoritative who/when/until record.

-- Drop due_at from book_copies (no data migration needed — docs/02 dropped it).
ALTER TABLE book_copies DROP COLUMN IF EXISTS due_at;

-- loans table (docs/02 §loans).
CREATE TABLE loans (
  id          UUID PRIMARY KEY,
  copy_id     UUID NOT NULL REFERENCES book_copies(id) ON DELETE CASCADE,
  user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  borrowed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  due_at      TIMESTAMPTZ NOT NULL,                   -- borrowed_at + LOAN_PERIOD (14d)
  returned_at TIMESTAMPTZ,                            -- NULL while the loan is open
  renewals    SMALLINT NOT NULL DEFAULT 0 CHECK (renewals >= 0),
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- At most one open loan per copy (docs/02 §5).
CREATE UNIQUE INDEX loans_open_copy_key ON loans (copy_id) WHERE returned_at IS NULL;
-- Fast borrow-limit check + my-loans list.
CREATE INDEX loans_user_open_idx ON loans (user_id, due_at) WHERE returned_at IS NULL;
-- Copy history / audit.
CREATE INDEX loans_copy_idx ON loans (copy_id, borrowed_at DESC);

-- updated_at trigger (same pattern as other tables, 0005_updated_at_triggers.up.sql).
CREATE OR REPLACE TRIGGER loans_updated_at
  BEFORE UPDATE ON loans
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

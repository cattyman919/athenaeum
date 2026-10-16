-- Undo 0007_loans: drop loans table, restore book_copies.due_at.

DROP TABLE IF EXISTS loans;

-- Restore the display-only due_at column that existed before M17.
ALTER TABLE book_copies ADD COLUMN IF NOT EXISTS due_at TIMESTAMPTZ;

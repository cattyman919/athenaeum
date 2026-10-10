-- Keeps updated_at current on every UPDATE for tables that carry the column.
CREATE FUNCTION set_updated_at() RETURNS trigger AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER users_set_updated_at        BEFORE UPDATE ON users        FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER books_set_updated_at        BEFORE UPDATE ON books        FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER book_copies_set_updated_at  BEFORE UPDATE ON book_copies  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER user_library_set_updated_at BEFORE UPDATE ON user_library FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER waitlist_set_updated_at     BEFORE UPDATE ON waitlist     FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER conversations_set_updated_at BEFORE UPDATE ON conversations FOR EACH ROW EXECUTE FUNCTION set_updated_at();

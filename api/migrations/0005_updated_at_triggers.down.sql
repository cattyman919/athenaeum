DROP TRIGGER IF EXISTS conversations_set_updated_at ON conversations;
DROP TRIGGER IF EXISTS waitlist_set_updated_at ON waitlist;
DROP TRIGGER IF EXISTS user_library_set_updated_at ON user_library;
DROP TRIGGER IF EXISTS book_copies_set_updated_at ON book_copies;
DROP TRIGGER IF EXISTS books_set_updated_at ON books;
DROP TRIGGER IF EXISTS users_set_updated_at ON users;
DROP FUNCTION IF EXISTS set_updated_at();

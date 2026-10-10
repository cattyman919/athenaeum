-- DDL verbatim from docs/02-data-model.md §4 (ordered by FK dependency).

CREATE TABLE users (
  id            UUID PRIMARY KEY,
  email         CITEXT NOT NULL,
  password_hash TEXT,                     -- NULL for SSO-only accounts (argon2id, docs/04)
  google_sub    TEXT,                     -- unique when present
  name          TEXT NOT NULL DEFAULT '',
  role          TEXT NOT NULL DEFAULT 'user' CHECK (role IN ('user','admin')),
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT users_google_sub_unique UNIQUE (google_sub)
);
CREATE UNIQUE INDEX users_email_lower_key ON users ((lower(email)::text));

CREATE TABLE refresh_tokens (
  id         UUID PRIMARY KEY,
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash TEXT NOT NULL,               -- sha256 of opaque refresh token
  expires_at TIMESTAMPTZ NOT NULL,        -- now + REFRESH_TTL (7d)
  revoked_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX refresh_tokens_hash_key ON refresh_tokens (token_hash);
CREATE INDEX refresh_tokens_user_idx ON refresh_tokens (user_id);

CREATE TABLE buildings (
  id UUID PRIMARY KEY,
  code TEXT NOT NULL,                    -- 'MAIN'
  name TEXT NOT NULL,                    -- 'Main Building'
  address TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE floors (
  id UUID PRIMARY KEY,
  building_id UUID NOT NULL REFERENCES buildings(id) ON DELETE CASCADE,
  floor_number INT NOT NULL,             -- starting 0 at lobby, 1..3 are stacks
  name TEXT NOT NULL,                    -- 'North Stacks'
  UNIQUE (building_id, floor_number)
);

CREATE TABLE sections (
  id UUID PRIMARY KEY,
  floor_id UUID NOT NULL REFERENCES floors(id) ON DELETE CASCADE,
  code TEXT NOT NULL,                    -- 'SF'
  name TEXT NOT NULL,                    -- 'Science Fiction & Speculative'
  color TEXT NOT NULL,                   -- canonical hex, docs/05 table
  UNIQUE (floor_id, code)
);

CREATE TABLE aisles (
  id UUID PRIMARY KEY,
  section_id UUID NOT NULL REFERENCES sections(id) ON DELETE CASCADE,
  code TEXT NOT NULL,                    -- 'A-1'
  UNIQUE (section_id, code)
);

CREATE TABLE shelves (
  id UUID PRIMARY KEY,
  aisle_id UUID NOT NULL REFERENCES aisles(id) ON DELETE CASCADE,
  code TEXT NOT NULL,                    -- 'S-2'
  row_level INT NOT NULL,                -- 4
  height_tier TEXT NOT NULL,             -- 'Eye Level'
  UNIQUE (aisle_id, code)
);
CREATE INDEX shelves_section_idx ON shelves (aisle_id);

CREATE TABLE books (
  id              UUID PRIMARY KEY,
  title           TEXT NOT NULL,
  subtitle        TEXT,
  authors         JSONB NOT NULL DEFAULT '[]',       -- ["Frank Herbert","..."]
  isbn13          TEXT,                               -- checksum-validated upstream, see docs/11
  isbn10          TEXT,
  publisher       TEXT,
  published_year  INT,
  description     TEXT,                               -- RAG chunk source, docs/06
  cover_url       TEXT,
  open_library_key TEXT,                              -- enrich provenance, docs/08
  dewey_num       TEXT,
  loc_num         TEXT,
  section_id      UUID REFERENCES sections(id),       -- default wayfinding section
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX books_isbn13_key ON books (isbn13) WHERE isbn13 IS NOT NULL;

CREATE TABLE book_copies (
  id               UUID PRIMARY KEY,
  book_id          UUID NOT NULL REFERENCES books(id) ON DELETE CASCADE,
  barcode          TEXT NOT NULL CHECK (barcode ~ '^ATH-[0-9]{6}-C[0-9]{2}$'),  -- ATH-002941-C1
  status           TEXT NOT NULL DEFAULT 'available'
                   CHECK (status IN ('available','checked_out','reserved_waitlist','missing_lost')),
  condition        TEXT NOT NULL DEFAULT 'good',
  shelf_id         UUID REFERENCES shelves(id),       -- NULL = in-catalog only
  due_at           TIMESTAMPTZ,                       -- set when checked_out (display only, docs/10)
  last_state_change TIMESTAMPTZ NOT NULL DEFAULT now(),
  created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (barcode)
);
CREATE INDEX book_copies_book_idx ON book_copies (book_id);
CREATE INDEX book_copies_shelf_idx ON book_copies (shelf_id) WHERE shelf_id IS NOT NULL;

CREATE TABLE user_library (
  id           UUID PRIMARY KEY,
  user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  book_id      UUID NOT NULL REFERENCES books(id) ON DELETE CASCADE,
  status       TEXT NOT NULL DEFAULT 'want_to_read'
               CHECK (status IN ('want_to_read','reading','read')),
  is_favorite  BOOL NOT NULL DEFAULT false,
  progress_pct SMALLINT CHECK (progress_pct BETWEEN 0 AND 100),
  rating       SMALLINT CHECK (rating BETWEEN 1 AND 5),
  notes        TEXT,
  added_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (user_id, book_id)
);
CREATE INDEX user_library_user_idx ON user_library (user_id, updated_at DESC);

CREATE TABLE waitlist (
  id         UUID PRIMARY KEY,
  book_id    UUID NOT NULL REFERENCES books(id) ON DELETE CASCADE,
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  status     TEXT NOT NULL DEFAULT 'waiting'
             CHECK (status IN ('waiting','ready','picked_up','expired','cancelled')),
  ready_at   TIMESTAMPTZ,
  expires_at TIMESTAMPTZ,                -- ready_at + 48h (docs/12 state machine)
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX waitlist_book_active_idx ON waitlist (book_id, created_at)
  WHERE status IN ('waiting','ready');

CREATE TABLE book_embeddings (
  id              BIGSERIAL PRIMARY KEY,
  book_id         UUID NOT NULL REFERENCES books(id) ON DELETE CASCADE,
  chunk_index     INT NOT NULL,
  content         TEXT NOT NULL,           -- chunked description (~300 tokens, docs/06)
  embedding       vector(768) NOT NULL,
  model           TEXT NOT NULL,           -- 'nomic-embed-text'
  chunker_version TEXT NOT NULL,           -- 'v1' - see docs/06 chunking
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (book_id, chunk_index)
);
CREATE INDEX book_embeddings_hnsw
  ON book_embeddings USING hnsw (embedding vector_cosine_ops)
  WITH (m = 16, ef_construction = 64);

CREATE TABLE conversations (
  id         UUID PRIMARY KEY,
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title      TEXT NOT NULL DEFAULT 'New conversation',   -- set from first message, docs/06
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX conversations_user_idx ON conversations (user_id, updated_at DESC);

CREATE TABLE messages (
  id              UUID PRIMARY KEY,
  conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  role            TEXT NOT NULL CHECK (role IN ('system','user','assistant','tool')),
  content         TEXT,
  tool_meta       JSONB,                  -- {tool_call_id, name, args, result} for tool messages
  tokens          INT,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX messages_conversation_idx ON messages (conversation_id, created_at);

CREATE TABLE copy_events (
  id          BIGSERIAL PRIMARY KEY,
  copy_id     UUID NOT NULL REFERENCES book_copies(id) ON DELETE CASCADE,
  from_status TEXT,                       -- NULL on initial insert
  to_status   TEXT NOT NULL,
  source      TEXT NOT NULL,              -- 'admin_patch' | 'scanner' | 'waitlist' | 'seed'
  actor_id    UUID REFERENCES users(id),
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX copy_events_recent_idx ON copy_events (created_at DESC);

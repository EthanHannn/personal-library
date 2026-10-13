CREATE TABLE readers (
    id TEXT PRIMARY KEY NOT NULL,
    username TEXT NOT NULL UNIQUE,
    nickname TEXT,
    legacy_role TEXT NOT NULL,
    portal_user TEXT UNIQUE,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
) STRICT;

CREATE TABLE books (
    id INTEGER PRIMARY KEY CHECK (id > 0),
    title TEXT NOT NULL,
    author TEXT NOT NULL,
    tag1 TEXT NOT NULL,
    tag2 TEXT NOT NULL,
    size TEXT NOT NULL,
    intro TEXT NOT NULL,
    status TEXT NOT NULL,
    submitted_by_id TEXT REFERENCES readers(id),
    post_id INTEGER,
    legacy_file_path TEXT,
    chapter_count INTEGER NOT NULL CHECK (chapter_count >= 0),
    word_count INTEGER NOT NULL CHECK (word_count >= 0),
    has_content INTEGER NOT NULL CHECK (has_content IN (0, 1)),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
) STRICT;
CREATE INDEX books_created ON books(created_at DESC, id DESC);
CREATE INDEX books_words ON books(word_count DESC, id DESC);

-- These totals already include the imported votes. Future vote changes apply
-- a delta against votes.imported_type; they must not add all votes again.
CREATE TABLE rating_snapshots (
    book_id INTEGER PRIMARY KEY REFERENCES books(id),
    popularity INTEGER NOT NULL,
    total_score INTEGER NOT NULL,
    score REAL NOT NULL,
    xiancao_count INTEGER NOT NULL,
    liangcao_count INTEGER NOT NULL,
    gancao_count INTEGER NOT NULL,
    kucao_count INTEGER NOT NULL,
    ducao_count INTEGER NOT NULL
) STRICT;
CREATE INDEX ratings_score ON rating_snapshots(score DESC, book_id DESC);

CREATE TABLE files (
    id INTEGER PRIMARY KEY,
    path TEXT NOT NULL UNIQUE,
    encoding TEXT NOT NULL CHECK (encoding IN ('utf-8', 'gb18030')),
    size INTEGER NOT NULL CHECK (size >= 0),
    modified_ns INTEGER NOT NULL,
    sha256 TEXT NOT NULL CHECK (length(sha256) = 64)
) STRICT;
CREATE TABLE book_contents (
    book_id INTEGER NOT NULL REFERENCES books(id),
    version INTEGER NOT NULL CHECK (version > 0),
    file_id INTEGER NOT NULL REFERENCES files(id),
    is_current INTEGER NOT NULL CHECK (is_current IN (0, 1)),
    PRIMARY KEY (book_id, version)
) STRICT;
CREATE UNIQUE INDEX book_current_content ON book_contents(book_id) WHERE is_current = 1;
CREATE TABLE chapters (
    id INTEGER PRIMARY KEY CHECK (id > 0),
    book_id INTEGER NOT NULL,
    content_version INTEGER NOT NULL,
    idx INTEGER NOT NULL CHECK (idx > 0),
    title TEXT NOT NULL,
    start_offset INTEGER NOT NULL CHECK (start_offset >= 0),
    end_offset INTEGER NOT NULL CHECK (end_offset > start_offset),
    FOREIGN KEY (book_id, content_version) REFERENCES book_contents(book_id, version),
    UNIQUE (book_id, content_version, idx)
) STRICT;

CREATE TABLE shelf_entries (
    id TEXT PRIMARY KEY NOT NULL,
    reader_id TEXT NOT NULL REFERENCES readers(id),
    book_id INTEGER NOT NULL REFERENCES books(id),
    added_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE (reader_id, book_id)
) STRICT;
CREATE INDEX shelf_recent ON shelf_entries(reader_id, added_at DESC, id);
CREATE TABLE reading_progress (
    id TEXT PRIMARY KEY NOT NULL,
    reader_id TEXT NOT NULL REFERENCES readers(id),
    book_id INTEGER NOT NULL REFERENCES books(id),
    chapter_idx INTEGER NOT NULL CHECK (chapter_idx > 0),
    percent REAL NOT NULL CHECK (percent >= 0 AND percent <= 1),
    updated_at INTEGER NOT NULL,
    content_version TEXT,
    anchor TEXT,
    revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0),
    UNIQUE (reader_id, book_id)
) STRICT;
CREATE INDEX progress_recent ON reading_progress(reader_id, updated_at DESC, id);
CREATE TABLE votes (
    id TEXT PRIMARY KEY NOT NULL,
    type TEXT NOT NULL,
    imported_type TEXT,
    reader_id TEXT NOT NULL REFERENCES readers(id),
    book_id INTEGER NOT NULL REFERENCES books(id),
    created_at INTEGER NOT NULL,
    UNIQUE (reader_id, book_id)
) STRICT;
CREATE INDEX votes_book ON votes(book_id);
CREATE TABLE comments (
    id TEXT PRIMARY KEY NOT NULL,
    content TEXT NOT NULL,
    reader_id TEXT NOT NULL REFERENCES readers(id),
    book_id INTEGER NOT NULL REFERENCES books(id),
    parent_id TEXT REFERENCES comments(id),
    like_count INTEGER NOT NULL,
    dislike_count INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    CHECK (parent_id IS NULL OR parent_id <> id)
) STRICT;
CREATE INDEX comments_book ON comments(book_id, created_at, id);
CREATE INDEX comments_parent ON comments(parent_id);
CREATE TABLE covers (
    book_id INTEGER PRIMARY KEY REFERENCES books(id),
    legacy_path TEXT,
    source TEXT,
    source_url TEXT,
    fetched_at INTEGER
) STRICT;

-- A legacy import owns a fresh target until every check succeeds. No runtime
-- service may open an unfinished import. Preserved IDs are identity mappings.
CREATE TABLE import_runs (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    source_sha256 TEXT NOT NULL,
    manifest_sha256 TEXT NOT NULL,
    source_version TEXT NOT NULL,
    portal_user TEXT NOT NULL,
    stage INTEGER NOT NULL DEFAULT 0,
    last_rowid INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL CHECK (status IN ('running', 'complete')),
    started_at INTEGER NOT NULL,
    completed_at INTEGER,
    report TEXT
) STRICT;

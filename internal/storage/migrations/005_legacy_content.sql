-- Keep legacy file IDs, encodings and unavailable chapter indexes.
-- Migrate owns the offline transaction and checks every FK before commit.
CREATE TABLE files_next (
    id INTEGER PRIMARY KEY,
    path TEXT NOT NULL UNIQUE,
    encoding TEXT NOT NULL CHECK (encoding IN ('utf-8', 'gb18030', 'utf-16le', 'utf-16be')),
    size INTEGER NOT NULL CHECK (size >= 0),
    modified_ns INTEGER NOT NULL,
    sha256 TEXT NOT NULL CHECK (length(sha256) = 64)
) STRICT;
INSERT INTO files_next SELECT * FROM files;
DROP TABLE files;
ALTER TABLE files_next RENAME TO files;

-- The old catalog retains chapter indexes for novels not uploaded to the
-- server. Preserve those versions without inventing a file or enabling reads.
CREATE TABLE book_contents_next (
    book_id INTEGER NOT NULL REFERENCES books(id),
    version INTEGER NOT NULL CHECK (version > 0),
    file_id INTEGER REFERENCES files(id),
    is_current INTEGER NOT NULL CHECK (is_current IN (0, 1)),
    PRIMARY KEY (book_id, version),
    CHECK (file_id IS NOT NULL OR is_current = 0)
) STRICT;
INSERT INTO book_contents_next SELECT * FROM book_contents;
DROP TABLE book_contents;
ALTER TABLE book_contents_next RENAME TO book_contents;
CREATE UNIQUE INDEX book_current_content ON book_contents(book_id) WHERE is_current = 1;

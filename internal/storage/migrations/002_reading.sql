-- The key is part of database backups. Signed block positions survive process
-- restarts, but cannot be forged into a byte offset inside a GB18030 character.
CREATE TABLE reader_keys (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    signing_key BLOB NOT NULL CHECK (length(signing_key) = 32)
) STRICT;
INSERT INTO reader_keys VALUES (1, randomblob(32));

CREATE TABLE progress_devices (
    reader_id TEXT NOT NULL REFERENCES readers(id),
    book_id INTEGER NOT NULL REFERENCES books(id),
    device_id TEXT NOT NULL,
    last_seq INTEGER NOT NULL CHECK (last_seq > 0),
    PRIMARY KEY (reader_id, book_id, device_id)
) STRICT;

-- Keep the latest 128 successful mutations per reader/book. The durable device
-- high-water mark rejects older retries even after their receipt is pruned.
CREATE TABLE progress_mutations (
    reader_id TEXT NOT NULL REFERENCES readers(id),
    book_id INTEGER NOT NULL REFERENCES books(id),
    mutation_id TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    revision INTEGER NOT NULL,
    response TEXT NOT NULL,
    PRIMARY KEY (reader_id, book_id, mutation_id)
) STRICT;
CREATE INDEX mutations_revision ON progress_mutations(reader_id, book_id, revision);

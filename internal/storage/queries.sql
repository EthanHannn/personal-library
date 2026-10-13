-- name: CurrentReader :one
SELECT id, username, nickname FROM readers WHERE portal_user = ?;

-- name: ListBooks :many
SELECT b.id, b.title, b.author, b.tag1, b.tag2, b.word_count, b.chapter_count,
       b.has_content, s.score
FROM books b JOIN rating_snapshots s ON s.book_id = b.id
WHERE b.status = 'APPROVED' AND b.id > sqlc.arg(after_id)
ORDER BY b.id LIMIT sqlc.arg(page_limit);

-- name: BookDetail :one
SELECT b.id, b.title, b.author, b.tag1, b.tag2, b.word_count, b.chapter_count,
       b.has_content, s.score, b.intro
FROM books b JOIN rating_snapshots s ON s.book_id = b.id
WHERE b.id = ? AND b.status = 'APPROVED';

-- name: ReadableBook :one
SELECT id, title, author, chapter_count FROM books
WHERE id = ? AND status = 'APPROVED' AND has_content = 1;

-- name: ChapterWindow :many
SELECT c.idx, c.title FROM chapters c
JOIN book_contents bc ON bc.book_id=c.book_id AND bc.version=c.content_version
WHERE c.book_id = sqlc.arg(book_id) AND bc.is_current=1 AND c.idx > sqlc.arg(after_idx)
ORDER BY c.idx LIMIT sqlc.arg(page_limit);

-- name: ReadChapter :one
SELECT b.id, b.title, b.author, b.chapter_count, c.idx, c.title AS chapter_title,
       c.start_offset, c.end_offset, f.path, f.encoding, f.size, f.modified_ns, f.sha256
FROM books b
JOIN book_contents bc ON bc.book_id=b.id AND bc.is_current=1
JOIN chapters c ON c.book_id=bc.book_id AND c.content_version=bc.version
JOIN files f ON f.id=bc.file_id
WHERE b.id = sqlc.arg(book_id) AND c.idx = sqlc.arg(chapter_idx)
  AND b.has_content=1 AND b.status='APPROVED';

-- name: ReaderKey :one
SELECT signing_key FROM reader_keys WHERE id=1;

-- name: SearchChapters :many
SELECT c.idx, c.title FROM chapters c
JOIN book_contents bc ON bc.book_id=c.book_id AND bc.version=c.content_version
WHERE c.book_id=sqlc.arg(book_id) AND bc.is_current=1 AND c.idx>sqlc.arg(after_idx)
AND instr(c.title,sqlc.arg(query))>0
ORDER BY c.idx LIMIT sqlc.arg(page_limit);

-- name: InShelf :one
SELECT EXISTS(SELECT 1 FROM shelf_entries WHERE reader_id=? AND book_id=?) AS in_shelf;

-- name: PreviousChapter :many
SELECT c.idx, c.title FROM chapters c
JOIN book_contents bc ON bc.book_id=c.book_id AND bc.version=c.content_version
WHERE c.book_id=sqlc.arg(book_id) AND bc.is_current=1 AND c.idx<sqlc.arg(chapter_idx)
ORDER BY c.idx DESC LIMIT 1;

-- name: ReadingProgress :one
SELECT chapter_idx, percent, updated_at, content_version, anchor, revision
FROM reading_progress WHERE reader_id=? AND book_id=?;

-- name: ProgressReceipt :one
SELECT request_hash, response FROM progress_mutations WHERE reader_id=? AND book_id=? AND mutation_id=?;

-- name: ProgressSequence :one
SELECT last_seq FROM progress_devices WHERE reader_id=? AND book_id=? AND device_id=?;

-- name: SaveProgress :exec
INSERT INTO reading_progress(id,reader_id,book_id,chapter_idx,percent,updated_at,content_version,anchor,revision)
VALUES(?,?,?,?,?,?,?,?,?)
ON CONFLICT(reader_id,book_id) DO UPDATE SET chapter_idx=excluded.chapter_idx,
percent=excluded.percent,updated_at=excluded.updated_at,content_version=excluded.content_version,
anchor=excluded.anchor,revision=excluded.revision;

-- name: SaveSequence :exec
INSERT INTO progress_devices(reader_id,book_id,device_id,last_seq) VALUES(?,?,?,?)
ON CONFLICT(reader_id,book_id,device_id) DO UPDATE SET last_seq=excluded.last_seq;

-- name: SaveReceipt :exec
INSERT INTO progress_mutations(reader_id,book_id,mutation_id,request_hash,revision,response) VALUES(?,?,?,?,?,?);

-- name: PruneReceipts :exec
DELETE FROM progress_mutations WHERE reader_id=? AND book_id=? AND revision<=?;

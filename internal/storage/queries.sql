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

-- name: AddToShelf :exec
INSERT INTO shelf_entries(id,reader_id,book_id,added_at,updated_at) VALUES(?,?,?,?,?)
ON CONFLICT(reader_id,book_id) DO NOTHING;

-- name: RemoveFromShelf :exec
DELETE FROM shelf_entries WHERE reader_id=? AND book_id=?;

-- name: CollectionCounts :one
SELECT
  (SELECT count(*) FROM shelf_entries s JOIN books b ON b.id=s.book_id
   WHERE s.reader_id=sqlc.arg(reader_id) AND b.status='APPROVED') AS shelf,
  (SELECT count(*) FROM reading_progress p JOIN books b ON b.id=p.book_id
   WHERE p.reader_id=sqlc.arg(reader_id) AND b.status='APPROVED') AS reading;

-- The timestamp and immutable entry ID together form the ordering key. Repeated
-- adds retain both; inserting or deleting another entry does not shift a page.
-- name: ShelfWindow :many
SELECT s.id AS entry_id, s.added_at, b.id, b.title, b.author, b.tag1, b.tag2,
       b.word_count, b.chapter_count, b.has_content, r.score,
       CAST(coalesce(p.chapter_idx,0) AS INTEGER) AS progress_chapter,
       CAST(coalesce(p.percent,0) AS REAL) AS progress_percent,
       CAST(coalesce(p.updated_at,0) AS INTEGER) AS progress_at
FROM shelf_entries s JOIN books b ON b.id=s.book_id
JOIN rating_snapshots r ON r.book_id=b.id
LEFT JOIN reading_progress p ON p.book_id=b.id AND p.reader_id=s.reader_id
WHERE s.reader_id=sqlc.arg(reader_id) AND b.status='APPROVED'
  AND (sqlc.arg(has_cursor)=0 OR s.added_at<sqlc.arg(after_time)
       OR (s.added_at=sqlc.arg(after_time) AND s.id>sqlc.arg(after_id)))
ORDER BY s.added_at DESC, s.id ASC LIMIT sqlc.arg(page_limit);

-- name: ReadingWindow :many
SELECT p.id AS entry_id, p.updated_at, p.chapter_idx, p.percent,
       b.id, b.title, b.author, b.tag1, b.tag2, b.word_count, b.chapter_count, b.has_content, r.score,
       CAST(EXISTS(SELECT 1 FROM shelf_entries s WHERE s.book_id=b.id AND s.reader_id=p.reader_id) AS INTEGER) AS in_shelf
FROM reading_progress p JOIN books b ON b.id=p.book_id
JOIN rating_snapshots r ON r.book_id=b.id
WHERE p.reader_id=sqlc.arg(reader_id) AND b.status='APPROVED'
  AND (sqlc.arg(has_cursor)=0 OR p.updated_at<sqlc.arg(after_time)
       OR (p.updated_at=sqlc.arg(after_time) AND p.id>sqlc.arg(after_id)))
ORDER BY p.updated_at DESC, p.id ASC LIMIT sqlc.arg(page_limit);

-- name: HomeStats :one
SELECT count(*) AS books, count(DISTINCT author) AS authors,
       count(DISTINCT nullif(tag1,'')) AS categories,
       CAST(coalesce(sum(has_content),0) AS INTEGER) AS readable
FROM books WHERE status='APPROVED';

-- name: FeaturedCandidates :many
SELECT b.id, b.title, b.author, b.tag1, b.tag2, b.word_count, b.chapter_count,
       b.has_content, r.score, r.xiancao_count, substr(b.intro,1,180) AS intro
FROM rating_snapshots r JOIN books b ON b.id=r.book_id
WHERE b.status='APPROVED' AND b.has_content=1 AND r.score>=7.5
ORDER BY r.xiancao_count DESC, r.score DESC, b.id DESC LIMIT 30;

-- name: HighRatedBooks :many
SELECT b.id, b.title, b.author, b.tag1, b.tag2, b.word_count, b.chapter_count,
       b.has_content, r.score
FROM rating_snapshots r JOIN books b ON b.id=r.book_id
WHERE b.status='APPROVED' AND b.has_content=1 AND r.score>=7.5
ORDER BY r.score DESC, b.id DESC LIMIT 6;

-- name: LatestBooks :many
SELECT b.id, b.title, b.author, b.tag1, b.tag2, b.word_count, b.chapter_count,
       b.has_content, r.score
FROM books b JOIN rating_snapshots r ON r.book_id=b.id
WHERE b.status='APPROVED' AND b.has_content=1 AND r.score>=7.5
ORDER BY b.created_at DESC, b.id DESC LIMIT 6;

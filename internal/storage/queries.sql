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
       c.start_offset, c.end_offset, f.path, f.encoding, f.size, f.modified_ns
FROM books b
JOIN book_contents bc ON bc.book_id=b.id AND bc.is_current=1
JOIN chapters c ON c.book_id=bc.book_id AND c.content_version=bc.version
JOIN files f ON f.id=bc.file_id
WHERE b.id = sqlc.arg(book_id) AND c.idx = sqlc.arg(chapter_idx)
  AND b.has_content=1 AND b.status='APPROVED';

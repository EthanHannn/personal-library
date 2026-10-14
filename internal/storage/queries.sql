-- name: CurrentReader :one
SELECT id, username, nickname FROM readers WHERE portal_user = ?;

-- name: VoteSnapshot :one
SELECT s.*, v.type AS my_vote
FROM books b JOIN rating_snapshots s ON s.book_id=b.id
LEFT JOIN votes v ON v.book_id=b.id AND v.reader_id=sqlc.arg(reader_id)
WHERE b.id=sqlc.arg(book_id) AND b.status='APPROVED';

-- name: VoteDeltas :one
SELECT CAST(COALESCE(sum((type='XIANCAO')-COALESCE(imported_type='XIANCAO',0)),0) AS INTEGER) AS xiancao,
       CAST(COALESCE(sum((type='LIANGCAO')-COALESCE(imported_type='LIANGCAO',0)),0) AS INTEGER) AS liangcao,
       CAST(COALESCE(sum((type='GANCAO')-COALESCE(imported_type='GANCAO',0)),0) AS INTEGER) AS gancao,
       CAST(COALESCE(sum((type='KUCAO')-COALESCE(imported_type='KUCAO',0)),0) AS INTEGER) AS kucao,
       CAST(COALESCE(sum((type='DUCAO')-COALESCE(imported_type='DUCAO',0)),0) AS INTEGER) AS ducao,
       CAST(COALESCE(sum(type NOT IN ('XIANCAO','LIANGCAO','GANCAO','KUCAO','DUCAO')
         OR COALESCE(imported_type NOT IN ('XIANCAO','LIANGCAO','GANCAO','KUCAO','DUCAO'),0)),0) AS INTEGER) AS invalid
FROM votes WHERE book_id=sqlc.arg(book_id);

-- name: SetVote :exec
INSERT INTO votes(id,type,reader_id,book_id,created_at) VALUES(?,?,?,?,?)
ON CONFLICT(reader_id,book_id) DO UPDATE SET type=excluded.type
WHERE votes.type<>excluded.type;

-- name: ListBooks :many
SELECT        CAST(EXISTS(SELECT 1 FROM cover_assets ca WHERE ca.book_id=b.id) OR EXISTS(SELECT 1 FROM covers cv WHERE cv.book_id=b.id AND cv.legacy_path LIKE '/covers/%' AND (cv.legacy_path LIKE '%.jpg' OR cv.legacy_path LIKE '%.jpeg' OR cv.legacy_path LIKE '%.png' OR cv.legacy_path LIKE '%.webp' OR cv.legacy_path LIKE '%.gif')) AS INTEGER) AS has_cover,
b.id, b.title, b.author, b.tag1, b.tag2, b.word_count, b.chapter_count,
       b.has_content, s.score
FROM books b JOIN rating_snapshots s ON s.book_id = b.id
WHERE b.status = 'APPROVED' AND b.id > sqlc.arg(after_id)
ORDER BY b.id LIMIT sqlc.arg(page_limit);

-- name: BookDetail :one
SELECT        CAST(EXISTS(SELECT 1 FROM cover_assets ca WHERE ca.book_id=b.id) OR EXISTS(SELECT 1 FROM covers cv WHERE cv.book_id=b.id AND cv.legacy_path LIKE '/covers/%' AND (cv.legacy_path LIKE '%.jpg' OR cv.legacy_path LIKE '%.jpeg' OR cv.legacy_path LIKE '%.png' OR cv.legacy_path LIKE '%.webp' OR cv.legacy_path LIKE '%.gif')) AS INTEGER) AS has_cover,
b.id, b.title, b.author, b.tag1, b.tag2, b.word_count, b.chapter_count,
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
SELECT        CAST(EXISTS(SELECT 1 FROM cover_assets ca WHERE ca.book_id=b.id) OR EXISTS(SELECT 1 FROM covers cv WHERE cv.book_id=b.id AND cv.legacy_path LIKE '/covers/%' AND (cv.legacy_path LIKE '%.jpg' OR cv.legacy_path LIKE '%.jpeg' OR cv.legacy_path LIKE '%.png' OR cv.legacy_path LIKE '%.webp' OR cv.legacy_path LIKE '%.gif')) AS INTEGER) AS has_cover,
s.id AS entry_id, s.added_at, b.id, b.title, b.author, b.tag1, b.tag2,
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
SELECT        CAST(EXISTS(SELECT 1 FROM cover_assets ca WHERE ca.book_id=b.id) OR EXISTS(SELECT 1 FROM covers cv WHERE cv.book_id=b.id AND cv.legacy_path LIKE '/covers/%' AND (cv.legacy_path LIKE '%.jpg' OR cv.legacy_path LIKE '%.jpeg' OR cv.legacy_path LIKE '%.png' OR cv.legacy_path LIKE '%.webp' OR cv.legacy_path LIKE '%.gif')) AS INTEGER) AS has_cover,
p.id AS entry_id, p.updated_at, p.chapter_idx, p.percent,
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
SELECT        CAST(EXISTS(SELECT 1 FROM cover_assets ca WHERE ca.book_id=b.id) OR EXISTS(SELECT 1 FROM covers cv WHERE cv.book_id=b.id AND cv.legacy_path LIKE '/covers/%' AND (cv.legacy_path LIKE '%.jpg' OR cv.legacy_path LIKE '%.jpeg' OR cv.legacy_path LIKE '%.png' OR cv.legacy_path LIKE '%.webp' OR cv.legacy_path LIKE '%.gif')) AS INTEGER) AS has_cover,
b.id, b.title, b.author, b.tag1, b.tag2, b.word_count, b.chapter_count,
       b.has_content, r.score, r.xiancao_count, substr(b.intro,1,180) AS intro
FROM rating_snapshots r JOIN books b ON b.id=r.book_id
WHERE b.status='APPROVED' AND b.has_content=1 AND r.score>=7.5
ORDER BY r.xiancao_count DESC, r.score DESC, b.id DESC LIMIT 30;

-- name: HighRatedBooks :many
SELECT        CAST(EXISTS(SELECT 1 FROM cover_assets ca WHERE ca.book_id=b.id) OR EXISTS(SELECT 1 FROM covers cv WHERE cv.book_id=b.id AND cv.legacy_path LIKE '/covers/%' AND (cv.legacy_path LIKE '%.jpg' OR cv.legacy_path LIKE '%.jpeg' OR cv.legacy_path LIKE '%.png' OR cv.legacy_path LIKE '%.webp' OR cv.legacy_path LIKE '%.gif')) AS INTEGER) AS has_cover,
b.id, b.title, b.author, b.tag1, b.tag2, b.word_count, b.chapter_count,
       b.has_content, r.score
FROM rating_snapshots r JOIN books b ON b.id=r.book_id
WHERE b.status='APPROVED' AND b.has_content=1 AND r.score>=7.5
ORDER BY r.score DESC, b.id DESC LIMIT 6;

-- name: LatestBooks :many
SELECT        CAST(EXISTS(SELECT 1 FROM cover_assets ca WHERE ca.book_id=b.id) OR EXISTS(SELECT 1 FROM covers cv WHERE cv.book_id=b.id AND cv.legacy_path LIKE '/covers/%' AND (cv.legacy_path LIKE '%.jpg' OR cv.legacy_path LIKE '%.jpeg' OR cv.legacy_path LIKE '%.png' OR cv.legacy_path LIKE '%.webp' OR cv.legacy_path LIKE '%.gif')) AS INTEGER) AS has_cover,
b.id, b.title, b.author, b.tag1, b.tag2, b.word_count, b.chapter_count,
       b.has_content, r.score
FROM books b JOIN rating_snapshots r ON r.book_id=b.id
WHERE b.status='APPROVED' AND b.has_content=1 AND r.score>=7.5
ORDER BY b.created_at DESC, b.id DESC LIMIT 6;

-- Catalog queries touch only book metadata, never the chapter index or text.
-- instr is a literal substring match, including Chinese single characters and
-- %, _ and quote characters. SQLite lower also folds ASCII author names.
-- name: CatalogStats :one
SELECT count(*) AS total, count(DISTINCT b.author) AS authors,
       CAST(coalesce(sum(b.has_content),0) AS INTEGER) AS readable
FROM books b JOIN rating_snapshots s ON s.book_id=b.id
WHERE b.status='APPROVED'
  AND (sqlc.arg(search)='' OR instr(lower(b.title),lower(sqlc.arg(search)))>0 OR instr(lower(b.author),lower(sqlc.arg(search)))>0)
  AND (sqlc.arg(author)='' OR b.author=sqlc.arg(author))
  AND (sqlc.arg(tag)='' OR b.tag1=sqlc.arg(tag) OR b.tag2=sqlc.arg(tag))
  AND (sqlc.arg(tag1)='' OR b.tag1=sqlc.arg(tag1))
  AND (sqlc.arg(tag2)='' OR b.tag2=sqlc.arg(tag2))
  AND s.score>=sqlc.arg(min_score) AND s.score<=sqlc.arg(max_score)
  AND b.word_count>=sqlc.arg(min_words) AND b.word_count<=sqlc.arg(max_words)
  AND (sqlc.arg(only_content)=0 OR b.has_content=1);

-- Every order ends in a unique key; page boundaries are deterministic even
-- when scores, word counts, titles or timestamps tie.
-- name: CatalogPage :many
WITH ordering AS (SELECT CAST(sqlc.arg(sort_by) AS TEXT) AS kind)
SELECT        CAST(EXISTS(SELECT 1 FROM cover_assets ca WHERE ca.book_id=b.id) OR EXISTS(SELECT 1 FROM covers cv WHERE cv.book_id=b.id AND cv.legacy_path LIKE '/covers/%' AND (cv.legacy_path LIKE '%.jpg' OR cv.legacy_path LIKE '%.jpeg' OR cv.legacy_path LIKE '%.png' OR cv.legacy_path LIKE '%.webp' OR cv.legacy_path LIKE '%.gif')) AS INTEGER) AS has_cover,
b.id, b.title, b.author, b.tag1, b.tag2, b.word_count, b.chapter_count,
       b.has_content, s.score, s.popularity, s.xiancao_count
FROM books b JOIN rating_snapshots s ON s.book_id=b.id CROSS JOIN ordering
WHERE b.status='APPROVED'
  AND (sqlc.arg(search)='' OR instr(lower(b.title),lower(sqlc.arg(search)))>0 OR instr(lower(b.author),lower(sqlc.arg(search)))>0)
  AND (sqlc.arg(author)='' OR b.author=sqlc.arg(author))
  AND (sqlc.arg(tag)='' OR b.tag1=sqlc.arg(tag) OR b.tag2=sqlc.arg(tag))
  AND (sqlc.arg(tag1)='' OR b.tag1=sqlc.arg(tag1))
  AND (sqlc.arg(tag2)='' OR b.tag2=sqlc.arg(tag2))
  AND s.score>=sqlc.arg(min_score) AND s.score<=sqlc.arg(max_score)
  AND b.word_count>=sqlc.arg(min_words) AND b.word_count<=sqlc.arg(max_words)
  AND (sqlc.arg(only_content)=0 OR b.has_content=1)
ORDER BY CASE WHEN ordering.kind='score' THEN s.score END DESC,
         CASE WHEN ordering.kind='xiancao' THEN s.xiancao_count END DESC,
         CASE WHEN ordering.kind='popularity' THEN s.popularity END DESC,
         CASE WHEN ordering.kind='latest' THEN b.created_at END DESC,
         CASE WHEN ordering.kind='words' THEN b.word_count END DESC,
         CASE WHEN ordering.kind='title' THEN b.title END ASC,
         b.id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CatalogCategories :many
SELECT tag1, tag2, count(*) AS total FROM books
WHERE status='APPROVED' AND tag1<>''
GROUP BY tag1, tag2 ORDER BY tag1, total DESC, tag2;

-- Recommendations use local metadata only. Unknown identities cannot create
-- matches, and absent candidates remain an empty list instead of random books.
-- name: RelatedBooks :many
SELECT        CAST(EXISTS(SELECT 1 FROM cover_assets ca WHERE ca.book_id=b.id) OR EXISTS(SELECT 1 FROM covers cv WHERE cv.book_id=b.id AND cv.legacy_path LIKE '/covers/%' AND (cv.legacy_path LIKE '%.jpg' OR cv.legacy_path LIKE '%.jpeg' OR cv.legacy_path LIKE '%.png' OR cv.legacy_path LIKE '%.webp' OR cv.legacy_path LIKE '%.gif')) AS INTEGER) AS has_cover,
b.id,b.title,b.author,b.tag1,b.tag2,b.word_count,b.chapter_count,b.has_content,s.score,
 CAST(CASE WHEN sqlc.arg(author)<>'' AND b.author=sqlc.arg(author) THEN 2 ELSE 1 END AS INTEGER) AS relevance,
 CAST(CASE WHEN sqlc.arg(words)>0 AND b.word_count>0 THEN abs(1.0*b.word_count/sqlc.arg(words)-1) ELSE 100 END AS REAL) AS distance
FROM books b JOIN rating_snapshots s ON s.book_id=b.id
WHERE b.id<>sqlc.arg(book_id) AND b.status='APPROVED' AND b.has_content=1
 AND (b.title<>sqlc.arg(title) OR b.author<>sqlc.arg(original_author))
 AND ((sqlc.arg(author)<>'' AND b.author=sqlc.arg(author))
   OR (sqlc.arg(tag1)<>'' AND (b.tag1=sqlc.arg(tag1) OR b.tag2=sqlc.arg(tag1)))
   OR (sqlc.arg(tag2)<>'' AND (b.tag1=sqlc.arg(tag2) OR b.tag2=sqlc.arg(tag2))))
ORDER BY relevance DESC, distance ASC, s.score DESC, b.id DESC LIMIT 6;

-- name: ProfileCounts :one
SELECT
 (SELECT count(*) FROM shelf_entries s JOIN books b ON b.id=s.book_id WHERE s.reader_id=sqlc.arg(reader_id) AND b.status='APPROVED') AS shelf,
 (SELECT count(*) FROM reading_progress p JOIN books b ON b.id=p.book_id WHERE p.reader_id=sqlc.arg(reader_id) AND b.status='APPROVED') AS reading,
 (SELECT count(*) FROM votes v JOIN books b ON b.id=v.book_id WHERE v.reader_id=sqlc.arg(reader_id) AND b.status='APPROVED' AND v.type IN ('XIANCAO','LIANGCAO','GANCAO','KUCAO','DUCAO')) AS votes;

-- name: PersonalVoteWindow :many
SELECT        CAST(EXISTS(SELECT 1 FROM cover_assets ca WHERE ca.book_id=b.id) OR EXISTS(SELECT 1 FROM covers cv WHERE cv.book_id=b.id AND cv.legacy_path LIKE '/covers/%' AND (cv.legacy_path LIKE '%.jpg' OR cv.legacy_path LIKE '%.jpeg' OR cv.legacy_path LIKE '%.png' OR cv.legacy_path LIKE '%.webp' OR cv.legacy_path LIKE '%.gif')) AS INTEGER) AS has_cover,
v.id AS entry_id, v.type, v.created_at, b.id,b.title,b.author,b.tag1,b.tag2,b.word_count,b.chapter_count,b.has_content,s.score
FROM votes v JOIN books b ON b.id=v.book_id JOIN rating_snapshots s ON s.book_id=b.id
WHERE v.reader_id=sqlc.arg(reader_id) AND b.status='APPROVED'
 AND v.type IN ('XIANCAO','LIANGCAO','GANCAO','KUCAO','DUCAO')
 AND (sqlc.arg(vote_type)='' OR v.type=sqlc.arg(vote_type))
 AND (sqlc.arg(has_cursor)=0 OR v.created_at<sqlc.arg(after_time) OR (v.created_at=sqlc.arg(after_time) AND v.id>sqlc.arg(after_id)))
ORDER BY v.created_at DESC,v.id ASC LIMIT sqlc.arg(page_limit);

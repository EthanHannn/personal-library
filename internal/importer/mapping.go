// Package importer copies immutable legacy snapshots into a separate, explicitly
// migrated database. Every batch and its checkpoint share one transaction.
package importer

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type mapping struct {
	name         string
	source       string
	fields       string
	predicate    string
	target       string
	targetFields string
	insert       string
	timestamps   []int
}

// Lists are explicit so schema drift fails visibly, and private login material
// can never be copied accidentally. All original primary keys are retained.
var mappings = []mapping{
	{"readers", "users", "id,username,nickname,role,CAST(createdAt AS TEXT),CAST(updatedAt AS TEXT)", "1", "readers", "id,username,nickname,legacy_role,created_at,updated_at", "INSERT INTO readers(id,username,nickname,legacy_role,created_at,updated_at) VALUES(?,?,?,?,?,?)", []int{4, 5}},
	{"books", "books", "id,title,author,tag1,tag2,size,intro,status,submittedById,postId,filePath,chapterCount,wordCount,CAST(hasContent AS INTEGER),CAST(createdAt AS TEXT),CAST(updatedAt AS TEXT)", "1", "books", "id,title,author,tag1,tag2,size,intro,status,submitted_by_id,post_id,legacy_file_path,chapter_count,word_count,has_content,created_at,updated_at", "INSERT INTO books VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)", []int{14, 15}},
	{"ratings", "books", "id,popularity,totalScore,CAST(score AS REAL),xiancaoCount,liangcaoCount,gancaoCount,kucaoCount,ducaoCount", "1", "rating_snapshots", "book_id,popularity,total_score,score,xiancao_count,liangcao_count,gancao_count,kucao_count,ducao_count", "INSERT INTO rating_snapshots VALUES(?,?,?,?,?,?,?,?,?)", nil},
	{"covers", "books", "id,coverPath,coverSource,coverSourceUrl,CAST(coverFetchedAt AS TEXT)", "1", "covers", "book_id,legacy_path,source,source_url,fetched_at", "INSERT INTO covers VALUES(?,?,?,?,?)", []int{4}},
	{"contents", "books", "id,replace(filePath,char(92),'/')", "hasContent=1", "book_contents bc JOIN files f ON f.id=bc.file_id", "bc.book_id,f.path", "INSERT INTO book_contents(book_id,version,file_id,is_current) VALUES(?1,1,(SELECT id FROM files WHERE path=?2),1)", nil},
	{"chapters", "chapters", "id,bookId,idx,title,startOffset,endOffset", "1", "chapters", "id,book_id,idx,title,start_offset,end_offset", "INSERT INTO chapters(id,book_id,idx,title,start_offset,end_offset,content_version) VALUES(?,?,?,?,?,?,1)", nil},
	{"shelf", "bookshelf_entries", "id,userId,bookId,CAST(addedAt AS TEXT),CAST(updatedAt AS TEXT)", "1", "shelf_entries", "id,reader_id,book_id,added_at,updated_at", "INSERT INTO shelf_entries VALUES(?,?,?,?,?)", []int{3, 4}},
	{"progress", "reading_progress", "id,userId,bookId,chapterIdx,CAST(percent AS REAL),CAST(updatedAt AS TEXT)", "1", "reading_progress", "id,reader_id,book_id,chapter_idx,percent,updated_at", "INSERT INTO reading_progress(id,reader_id,book_id,chapter_idx,percent,updated_at) VALUES(?,?,?,?,?,?)", []int{5}},
	{"votes", "votes", "id,type,userId,bookId,CAST(createdAt AS TEXT)", "1", "votes", "id,type,reader_id,book_id,created_at", "INSERT INTO votes(id,type,reader_id,book_id,created_at,imported_type) VALUES(?1,?2,?3,?4,?5,?2)", []int{4}},
	{"comments", "comments", "id,content,userId,bookId,likeCount,dislikeCount,CAST(createdAt AS TEXT),CAST(updatedAt AS TEXT)", "1", "comments", "id,content,reader_id,book_id,like_count,dislike_count,created_at,updated_at", "INSERT INTO comments(id,content,reader_id,book_id,like_count,dislike_count,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)", []int{6, 7}},
	// Replies can precede their parents in the source. Resolve them only after
	// all comment rows exist, keeping FK enforcement enabled throughout.
	{"replies", "comments", "id,parentId", "parentId IS NOT NULL", "comments WHERE parent_id IS NOT NULL", "id,parent_id", "UPDATE comments SET parent_id=?2 WHERE id=?1", nil},
}

func (m mapping) normalize(values []any) error {
	for _, i := range m.timestamps {
		if values[i] == nil {
			continue
		}
		raw, ok := values[i].(string)
		if !ok {
			return fmt.Errorf("unexpected timestamp storage type")
		}
		stamp, err := timestamp(raw)
		if err != nil {
			return err
		}
		values[i] = stamp
	}
	return nil
}

// Prisma writes epoch milliseconds; SQLite defaults and old scripts also write
// UTC SQL strings or RFC3339. Unknown values fail instead of becoming time zero.
func timestamp(raw string) (int64, error) {
	if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
		// Integers follow Prisma's millisecond contract; never guess another
		// unit from magnitude. Reject values outside the supported UTC range.
		if n < 0 || n > 253402300799999 {
			return 0, fmt.Errorf("timestamp out of range")
		}
		return n, nil
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05.999999999", "2006-01-02T15:04:05.999999999"} {
		if t, err := time.Parse(layout, strings.TrimSpace(raw)); err == nil {
			return t.UnixMilli(), nil
		}
	}
	return 0, fmt.Errorf("unrecognized legacy timestamp")
}

package service

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/EthanHannn/personal-library/internal/gateway"
	"github.com/EthanHannn/personal-library/internal/storage"
	"github.com/EthanHannn/personal-library/internal/storage/dbgen"
)

type profileAPI struct {
	store                            *storage.Repository
	readerID, portalUser, accountURL string
}

func (a profileAPI) profile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	counts, err := a.store.Queries.ProfileCounts(ctx, a.readerID)
	if err != nil {
		apiError(w, err)
		return
	}
	user, err := a.store.Queries.CurrentReader(ctx, sql.NullString{String: a.portalUser, Valid: true})
	if err != nil {
		apiError(w, err)
		return
	}
	reading, err := (collectionAPI{store: a.store, readerID: a.readerID}).page(ctx, "reading", collectionCursor{}, 3)
	if err != nil {
		apiError(w, err)
		return
	}
	nickname := user.Nickname.String
	if nickname == "" {
		nickname = user.Username
	}
	jsonResponse(w, map[string]any{"nickname": nickname, "accountUrl": a.accountURL, "counts": map[string]int64{"shelf": counts.Shelf, "reading": counts.Reading, "votes": counts.Votes}, "reading": reading.Entries})
}

func (a profileAPI) votes(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("type")
	if kind != "" && !storage.VoteType(kind).Valid() {
		gateway.Error(w, 400, "invalid_vote", "评价筛选无效")
		return
	}
	cursor, err := parseCollectionCursor(r.URL.Query().Get("cursor"), "votes:"+kind)
	if err != nil {
		gateway.Error(w, 400, "invalid_cursor", "翻页位置无效，请返回第一页")
		return
	}
	limit, err := integer(r.URL.Query().Get("limit"), 24, 1, 48)
	if err != nil {
		gateway.Error(w, 400, "invalid_window", "每次最多加载 48 条评价")
		return
	}
	hasCursor := 0
	if cursor.ID != "" {
		hasCursor = 1
	}
	rows, err := a.store.Queries.PersonalVoteWindow(r.Context(), dbgen.PersonalVoteWindowParams{ReaderID: a.readerID, VoteType: kind, HasCursor: hasCursor, AfterTime: cursor.At, AfterID: cursor.ID, PageLimit: limit + 1})
	if err != nil {
		apiError(w, err)
		return
	}
	type entry struct {
		Book      Book   `json:"book"`
		Type      string `json:"type"`
		CreatedAt int64  `json:"createdAt"`
	}
	entries := make([]entry, 0, len(rows))
	var next *string
	if int64(len(rows)) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		next = nextCollectionCursor("votes:"+kind, last.CreatedAt, last.EntryID)
	}
	for _, b := range rows {
		entries = append(entries, entry{Book: Book{ID: b.ID, Title: b.Title, Author: b.Author, Tag1: b.Tag1, Tag2: b.Tag2, WordCount: b.WordCount, ChapterCount: b.ChapterCount, HasContent: b.HasContent == 1, Score: b.Score}, Type: b.Type, CreatedAt: b.CreatedAt})
	}
	jsonResponse(w, map[string]any{"entries": entries, "nextCursor": next})
}

func (a profileAPI) related(w http.ResponseWriter, r *http.Request) {
	id, err := integer(r.PathValue("id"), 0, 1, 1<<53-1)
	if err != nil {
		gateway.Error(w, 400, "invalid_book", "作品编号无效")
		return
	}
	b, err := a.store.Queries.BookDetail(r.Context(), id)
	if err != nil {
		apiError(w, err)
		return
	}
	rows, err := a.store.Queries.RelatedBooks(r.Context(), dbgen.RelatedBooksParams{BookID: id, Title: b.Title, OriginalAuthor: b.Author, Author: recommendationIdentity(b.Author), Words: b.WordCount, Tag1: recommendationIdentity(b.Tag1), Tag2: recommendationIdentity(b.Tag2)})
	if err != nil {
		apiError(w, err)
		return
	}
	type related struct {
		Book   Book   `json:"book"`
		Reason string `json:"reason"`
	}
	entries := make([]related, 0, len(rows))
	for _, b := range rows {
		reason := "同类作品"
		if b.Relevance == 2 {
			reason = "同一作者"
		}
		entries = append(entries, related{Book: Book{ID: b.ID, Title: b.Title, Author: b.Author, Tag1: b.Tag1, Tag2: b.Tag2, WordCount: b.WordCount, ChapterCount: b.ChapterCount, HasContent: b.HasContent == 1, Score: b.Score}, Reason: reason})
	}
	jsonResponse(w, map[string]any{"entries": entries})
}

func recommendationIdentity(value string) string {
	switch strings.TrimSpace(value) {
	case "", "未知", "未知作者", "佚名", "不详", "其他", "未分类":
		return ""
	}
	return value
}

package service

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/EthanHannn/personal-library/internal/gateway"
	"github.com/EthanHannn/personal-library/internal/storage"
)

type voteAPI struct {
	store    *storage.Repository
	readerID string
}

func (a voteAPI) vote(w http.ResponseWriter, r *http.Request) {
	id, err := integer(r.PathValue("id"), 0, 1, 1<<53-1)
	if err != nil {
		gateway.Error(w, 400, "invalid_book", "作品编号无效")
		return
	}
	if r.URL.RawQuery != "" {
		gateway.Error(w, 400, "invalid_vote", "投票请求不接受查询参数")
		return
	}
	var result storage.VoteSummary
	if r.Method == http.MethodGet {
		result, err = a.store.VoteSummary(r.Context(), a.readerID, id)
	} else {
		var body struct {
			Type storage.VoteType `json:"type"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
		dec.DisallowUnknownFields()
		var trailing any
		if dec.Decode(&body) != nil || !body.Type.Valid() || dec.Decode(&trailing) != io.EOF {
			gateway.Error(w, 400, "invalid_vote", "请选择仙草、粮草、干草、枯草或毒草")
			return
		}
		result, err = a.store.SetVote(r.Context(), a.readerID, id, body.Type)
	}
	if err != nil {
		apiError(w, err)
		return
	}
	jsonResponse(w, result)
}

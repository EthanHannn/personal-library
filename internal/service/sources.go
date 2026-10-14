package service

import (
	"github.com/EthanHannn/personal-library/internal/management"
	"net/http"
)

func (a manageAPI) sources(w http.ResponseWriter, r *http.Request) {
	if !a.available(w) {
		return
	}
	sources, err := a.jobs.Sources(r.Context())
	if err != nil {
		apiError(w, err)
		return
	}
	jsonResponse(w, map[string]any{"sources": sources, "templates": management.SourceTemplates()})
}
func (a manageAPI) saveSource(w http.ResponseWriter, r *http.Request) {
	if !a.available(w) {
		return
	}
	var body struct {
		Config          management.SourceConfig `json:"config"`
		Enabled         bool                    `json:"enabled"`
		ExpectedVersion int                     `json:"expectedVersion"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	source, err := a.jobs.SaveSource(r.Context(), r.PathValue("id"), body.ExpectedVersion, body.Enabled, body.Config)
	if err != nil {
		manageError(w, err)
		return
	}
	jsonResponse(w, source)
}

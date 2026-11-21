package http

import (
	"net/http"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/platform"
)

// listMeta is the pagination block of every list response (docs/API.md).
type listMeta struct {
	NextCursor string `json:"next_cursor,omitempty"`
	Limit      int    `json:"limit"`
	HasMore    bool   `json:"has_more"`
}

type listBody struct {
	Data any      `json:"data"`
	Meta listMeta `json:"meta"`
}

// pageFromQuery reads ?limit and ?cursor.
func pageFromQuery(r *http.Request) (domain.Page, error) {
	limit, err := platform.ParseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		return domain.Page{}, err
	}
	p := domain.Page{Limit: limit}
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		c, err := platform.DecodeCursor(raw)
		if err != nil {
			return domain.Page{}, err
		}
		p.After = &c
	}
	return p, nil
}

func respondList(w http.ResponseWriter, items any, limit int, next *domain.Cursor) {
	meta := listMeta{Limit: limit, HasMore: next != nil}
	if next != nil {
		meta.NextCursor = platform.EncodeCursor(*next)
	}
	writeJSON(w, http.StatusOK, listBody{Data: items, Meta: meta})
}

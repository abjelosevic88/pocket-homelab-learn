package server

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/abjelosevic88/pocket-homelab-learn/internal/store"
)

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func (s *Server) handleListDecks(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	st, _ := s.store.GetSettings(ctx, uid)
	decks, _, err := s.decksWithCounts(ctx, uid, st, queryBool(r, "archived"))
	if err != nil {
		s.fail(w, err, "decks")
		return
	}
	writeJSON(w, http.StatusOK, decks)
}

func (s *Server) handleGetDeck(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	st, _ := s.store.GetSettings(ctx, uid)
	decks, _, err := s.decksWithCounts(ctx, uid, st, true)
	if err != nil {
		s.fail(w, err, "decks")
		return
	}
	id := idParam(r, "id")
	for _, d := range decks {
		if d.ID == id {
			writeJSON(w, http.StatusOK, d)
			return
		}
	}
	writeErr(w, http.StatusNotFound, "not found")
}

type deckInput struct {
	Name          string  `json:"name"`
	Description   string  `json:"description"`
	Color         string  `json:"color"`
	NewPerDay     int     `json:"new_per_day"`
	ReviewsPerDay int     `json:"reviews_per_day"`
	Retention     float64 `json:"retention"`
	NewOrder      string  `json:"new_order"`
	Archived      bool    `json:"archived"`
}

// apply copies the sent fields onto d and says what is wrong ("" = fine).
func (in deckInput) apply(d *store.Deck, keys map[string]bool) string {
	if keys["name"] {
		d.Name = strings.TrimSpace(in.Name)
	}
	if d.Name == "" || len([]rune(d.Name)) > 120 {
		return "a deck needs a name (up to 120 characters)"
	}
	if keys["description"] {
		d.Description = strings.TrimSpace(in.Description)
	}
	if keys["color"] {
		if in.Color != "" && !hexColor.MatchString(in.Color) {
			return "colour is #rrggbb"
		}
		d.Color = in.Color
	}
	if keys["new_per_day"] {
		if in.NewPerDay < 0 || in.NewPerDay > 9999 {
			return "new cards a day is 0 to 9999"
		}
		d.NewPerDay = in.NewPerDay
	}
	if keys["reviews_per_day"] {
		if in.ReviewsPerDay < 0 || in.ReviewsPerDay > 99999 {
			return "reviews a day is 0 to 99999"
		}
		d.ReviewsPerDay = in.ReviewsPerDay
	}
	if keys["retention"] {
		if in.Retention != 0 && (in.Retention < 0.7 || in.Retention > 0.99) {
			return "retention is 70% to 99% (or 0 for your default)"
		}
		d.Retention = in.Retention
	}
	if keys["new_order"] {
		if in.NewOrder != "added" && in.NewOrder != "random" {
			return "new cards come in the order added or random"
		}
		d.NewOrder = in.NewOrder
	}
	if keys["archived"] {
		d.Archived = in.Archived
	}
	return ""
}

func (s *Server) handleCreateDeck(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	st, _ := s.store.GetSettings(ctx, uid)
	var in deckInput
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	d := &store.Deck{NewPerDay: st.NewPerDay, ReviewsPerDay: st.ReviewsPerDay, NewOrder: "added"}
	if msg := in.apply(d, keys); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.CreateDeck(ctx, uid, d); err != nil {
		if err == store.ErrConflict {
			writeErr(w, http.StatusConflict, "you already have a deck with that name")
			return
		}
		s.fail(w, err, "create deck")
		return
	}
	writeJSON(w, http.StatusCreated, d)
}

func (s *Server) handleUpdateDeck(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	d, err := s.store.GetDeck(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "deck")
		return
	}
	var in deckInput
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if msg := in.apply(d, keys); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.UpdateDeck(ctx, uid, d); err != nil {
		if err == store.ErrConflict {
			writeErr(w, http.StatusConflict, "you already have a deck with that name")
			return
		}
		s.fail(w, err, "update deck")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleDeleteDeck(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := s.store.DeleteDeck(ctx, actorID(ctx), idParam(r, "id")); err != nil {
		s.fail(w, err, "delete deck")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleReorderDecks(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	ctx := r.Context()
	if err := s.store.ReorderDecks(ctx, actorID(ctx), in.IDs); err != nil {
		s.fail(w, err, "reorder")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleExtendDeck lets a person study more today: extra new cards or reviews.
func (s *Server) handleExtendDeck(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	d, err := s.store.GetDeck(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "deck")
		return
	}
	var in struct {
		New     int `json:"new"`
		Reviews int `json:"reviews"`
	}
	if err := decode(r, &in); err != nil || in.New < 0 || in.Reviews < 0 || in.New > 9999 || in.Reviews > 99999 {
		writeErr(w, http.StatusBadRequest, "say how many more new cards or reviews")
		return
	}
	st, _ := s.store.GetSettings(ctx, uid)
	key := extendKey(uid, d.ID, s.clockFor(st).today())
	var cur struct{ New, Reviews int }
	if v, _, ok := s.store.CacheGet(ctx, key); ok {
		_ = json.Unmarshal([]byte(v), &cur)
	}
	cur.New += in.New
	cur.Reviews += in.Reviews
	b, _ := json.Marshal(cur)
	if err := s.store.CachePut(ctx, key, string(b)); err != nil {
		s.fail(w, err, "extend")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "new": cur.New, "reviews": cur.Reviews})
}

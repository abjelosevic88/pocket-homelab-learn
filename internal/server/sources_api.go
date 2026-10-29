package server

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/abjelosevic88/pocket-homelab-learn/internal/pocket"
	"github.com/abjelosevic88/pocket-homelab-learn/internal/store"
)

// ---- cards from other Pocket apps ----
//
// Highlights from Pocket Reader (articles) and Pocket Library (books), and
// notes from Pocket Notes, are listed here so they can become cards: as
// they are (the passage on the front, where it is from on the back) or
// through the AI, which writes questions about them.

// source is one passage a card could be made from.
type source struct {
	ID       string `json:"id"` // "reader:12", "library:7", "notes:3"
	App      string `json:"app"`
	Text     string `json:"text"`
	Note     string `json:"note,omitempty"`
	Title    string `json:"title"`
	URL      string `json:"url,omitempty"`
	Date     string `json:"date,omitempty"`
	Added    bool   `json:"added"` // already a card
	OpenURL  string `json:"open_url,omitempty"`
	Category string `json:"category,omitempty"`
}

func (s *Server) handleSources(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	app, ok := appBySlug(chiParam(r, "app"))
	if !ok {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	cl := s.clients(ctx, uid)
	conn, err := cl.conn(ctx, app, app.scopes...)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"connected": false, "detail": err.Error(), "items": []source{}})
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	limit := queryInt(r, "limit")
	if limit <= 0 || limit > 200 {
		limit = 60
	}
	items := []source{}
	switch app.slug {
	case "reader":
		hs, e := conn.client.Highlights(ctx, q, limit)
		err = e
		for _, h := range hs {
			items = append(items, source{ID: fmt.Sprintf("reader:%d", h.ID), App: "reader", Text: h.Text, Note: h.Note, Title: firstNonEmpty(h.ArticleTitle, h.SiteName), URL: h.ArticleURL, Date: h.CreatedAt,
				OpenURL: strings.TrimRight(conn.public, "/") + fmt.Sprintf("/a/%d", h.ArticleID)})
		}
	case "library":
		as, e := conn.client.Annotations(ctx, q, limit)
		err = e
		for _, a := range as {
			if a.Kind == "bookmark" || strings.TrimSpace(a.Text+a.Note) == "" {
				continue
			}
			title := a.BookTitle
			if a.Chapter != "" {
				title += " — " + a.Chapter
			}
			items = append(items, source{ID: fmt.Sprintf("library:%d", a.ID), App: "library", Text: firstNonEmpty(a.Text, a.Note), Note: noteIfText(a), Title: title, Date: a.CreatedAt,
				OpenURL: strings.TrimRight(conn.public, "/") + fmt.Sprintf("/book/%d", a.BookID)})
		}
	case "notes":
		ns, e := conn.client.SearchNotes(ctx, q, limit)
		err = e
		for _, n := range ns {
			items = append(items, source{ID: fmt.Sprintf("notes:%d", n.ID), App: "notes", Text: n.Snippet, Title: firstNonEmpty(n.Title, "Untitled"), Category: n.Category,
				OpenURL: strings.TrimRight(conn.public, "/") + fmt.Sprintf("/note/%d", n.ID)})
		}
	}
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"connected": false, "detail": app.name + ": " + err.Error(), "items": []source{}})
		return
	}
	for i := range items {
		if _, e := s.store.NoteByExternal(ctx, uid, "highlight", items[i].ID); e == nil {
			items[i].Added = true
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"connected": true, "via": conn.via, "items": items})
}

func noteIfText(a pocket.Annotation) string {
	if a.Text != "" {
		return a.Note
	}
	return ""
}

// handleSourceNote reads one note from Pocket Notes (for the AI to make cards from).
func (s *Server) handleSourceNote(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	conn, err := s.clients(ctx, uid).conn(ctx, appNotes, appNotes.scopes...)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	n, err := conn.client.GetNote(ctx, idParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadGateway, "Pocket Notes: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": n.ID, "title": n.Title, "content": n.Content, "category": n.Category,
		"open_url": strings.TrimRight(conn.public, "/") + fmt.Sprintf("/note/%d", n.ID)})
}

// handleAddHighlights adds highlights as cards as they are: the passage on
// the front, its source (and your own note) on the back.
func (s *Server) handleAddHighlights(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in struct {
		DeckID int64    `json:"deck_id"`
		Deck   string   `json:"deck"`
		Items  []source `json:"items"`
		Tags   string   `json:"tags"`
	}
	if err := decode(r, &in); err != nil || len(in.Items) == 0 || len(in.Items) > 500 {
		writeErr(w, http.StatusBadRequest, "choose up to 500 highlights")
		return
	}
	d, err := s.deckFor(ctx, uid, in.DeckID, in.Deck, "highlight")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "choose a deck")
		return
	}
	var items []store.ImportItem
	for _, h := range in.Items {
		if !strings.HasPrefix(h.ID, "reader:") && !strings.HasPrefix(h.ID, "library:") {
			continue
		}
		back := h.Title
		if h.Note != "" {
			back = strings.TrimSpace(h.Note + "\n\n— " + h.Title)
		} else if back != "" {
			back = "— " + back
		}
		if back == "" {
			back = "—"
		}
		n := store.Note{DeckID: d.ID, Kind: store.KindBasic, Front: h.Text, Back: back, Tags: strings.TrimSpace(in.Tags + " " + h.App),
			Source: "highlight", ExternalID: h.ID, SourceRef: h.Title, SourceURL: firstNonEmpty(h.URL, h.OpenURL)}
		if msg := checkNote(&n); msg != "" {
			continue
		}
		items = append(items, store.ImportItem{Note: n, Cards: []store.Card{{Ord: 0}}})
	}
	created, already, err := s.store.ImportBatch(ctx, uid, items)
	if err != nil {
		s.fail(w, err, "add highlights")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"notes": created, "already": already, "deck_id": d.ID, "deck": d.Name})
}

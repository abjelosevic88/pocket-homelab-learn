package server

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/abjelosevic88/pocket-homelab-learn/internal/store"
)

const maxField = 20000

var clozeRe = regexp.MustCompile(`\{\{c(\d{1,3})::`)

// clozeNumbers are the cloze numbers used in a text ({{c1::…}}, {{c2::…}}).
func clozeNumbers(text string) []int {
	seen := map[int]bool{}
	var out []int
	for _, m := range clozeRe.FindAllStringSubmatch(text, -1) {
		n, _ := strconv.Atoi(m[1])
		if n > 0 && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.Ints(out)
	return out
}

// cardOrds are the cards a note makes.
func cardOrds(kind, front string) []int {
	switch kind {
	case store.KindReverse:
		return []int{0, 1}
	case store.KindCloze:
		return clozeNumbers(front)
	}
	return []int{0}
}

// checkNote says what is wrong with a note ("" = fine).
func checkNote(n *store.Note) string { return checkNoteBack(n, false) }

// checkNoteBack is checkNote; imports may keep an empty back (Anki allows
// it: a sound or a picture on the front alone).
func checkNoteBack(n *store.Note, emptyBack bool) string {
	n.Front, n.Back, n.Extra = strings.TrimSpace(n.Front), strings.TrimSpace(n.Back), strings.TrimSpace(n.Extra)
	if n.Kind == "" {
		n.Kind = store.KindBasic
	}
	if !store.ValidKind(n.Kind) {
		return "a card is basic, reverse, cloze or type"
	}
	if n.Front == "" {
		return "the front is empty"
	}
	if n.Kind != store.KindCloze && n.Back == "" && !emptyBack {
		return "the back is empty"
	}
	if n.Kind == store.KindCloze && len(clozeNumbers(n.Front)) == 0 {
		return "a cloze card needs a gap like {{c1::word}}"
	}
	if len(n.Front) > maxField || len(n.Back) > maxField || len(n.Extra) > maxField {
		return "a side is at most 20,000 characters"
	}
	if len(n.Tags) > 1000 || len(n.SourceRef) > 500 || len(n.SourceURL) > 2000 || len(n.ExternalID) > 200 {
		return "tags or the source are too long"
	}
	return ""
}

type noteInput struct {
	DeckID     int64  `json:"deck_id"`
	Deck       string `json:"deck"` // a deck by name, created when missing
	Kind       string `json:"kind"`
	Front      string `json:"front"`
	Back       string `json:"back"`
	Extra      string `json:"extra"`
	Tags       string `json:"tags"`
	SourceRef  string `json:"source_ref"`
	SourceURL  string `json:"source_url"`
	ExternalID string `json:"external_id"`
	Source     string `json:"source"`
}

// deckFor resolves a deck id or name (creating a named deck the first time).
func (s *Server) deckFor(ctx context.Context, uid, id int64, name, source string) (*store.Deck, error) {
	if id > 0 {
		return s.store.GetDeck(ctx, uid, id)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("choose a deck")
	}
	if d, err := s.store.GetDeckByName(ctx, uid, name); err == nil {
		return d, nil
	}
	st, _ := s.store.GetSettings(ctx, uid)
	d := &store.Deck{Name: name, NewPerDay: st.NewPerDay, ReviewsPerDay: st.ReviewsPerDay, Source: source}
	if len([]rune(name)) > 120 {
		return nil, errors.New("a deck name is up to 120 characters")
	}
	if err := s.store.CreateDeck(ctx, uid, d); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return s.store.GetDeckByName(ctx, uid, name)
		}
		return nil, err
	}
	return d, nil
}

// noteSources are what a person (or the app itself) may set as a note's source.
var noteSources = map[string]bool{"app": true, "ai": true, "highlight": true, "csv": true}

func (s *Server) handleCreateNote(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in noteInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	src := "app"
	if noteSources[in.Source] {
		src = in.Source
	}
	if app := linkApp(ctx); app != "" {
		src = app // what another app adds stays marked as its own
	}
	d, err := s.deckFor(ctx, uid, in.DeckID, in.Deck, src)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusBadRequest, "no such deck")
			return
		}
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	n := &store.Note{DeckID: d.ID, Kind: in.Kind, Front: in.Front, Back: in.Back, Extra: in.Extra, Tags: in.Tags,
		Source: src, SourceRef: strings.TrimSpace(in.SourceRef), SourceURL: strings.TrimSpace(in.SourceURL), ExternalID: strings.TrimSpace(in.ExternalID)}
	if msg := checkNote(n); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if n.ExternalID != "" {
		if old, err := s.store.NoteByExternal(ctx, uid, n.Source, n.ExternalID); err == nil {
			full, _ := s.store.GetNote(ctx, uid, old.ID)
			writeJSON(w, http.StatusOK, map[string]any{"note": full, "duplicate": true})
			return
		}
	}
	if err := s.store.CreateNote(ctx, uid, n, cardOrds(n.Kind, n.Front)); err != nil {
		s.fail(w, err, "create note")
		return
	}
	n.DeckName = d.Name
	if app := linkApp(ctx); app != "" {
		s.publish(ctx, uid, "learn.cards_added", "1 card added to "+d.Name, map[string]any{"deck": d.Name, "deck_id": d.ID, "notes": 1, "by": app})
	}
	writeJSON(w, http.StatusCreated, map[string]any{"note": n, "duplicate": false})
}

func (s *Server) handleGetNote(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	n, err := s.store.GetNote(ctx, actorID(ctx), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "note")
		return
	}
	if app := linkApp(ctx); app != "" && n.Source != app {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, n)
}

func (s *Server) handleUpdateNote(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	n, err := s.store.GetNote(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "note")
		return
	}
	if app := linkApp(ctx); app != "" && n.Source != app {
		writeErr(w, http.StatusForbidden, "this card was not added by "+app)
		return
	}
	var in noteInput
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if keys["deck_id"] || keys["deck"] {
		d, err := s.deckFor(ctx, uid, in.DeckID, in.Deck, n.Source)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "no such deck")
			return
		}
		n.DeckID = d.ID
	}
	set := func(k string, dst *string, v string) {
		if keys[k] {
			*dst = v
		}
	}
	set("kind", &n.Kind, in.Kind)
	set("front", &n.Front, in.Front)
	set("back", &n.Back, in.Back)
	set("extra", &n.Extra, in.Extra)
	set("tags", &n.Tags, in.Tags)
	set("source_ref", &n.SourceRef, strings.TrimSpace(in.SourceRef))
	set("source_url", &n.SourceURL, strings.TrimSpace(in.SourceURL))
	if msg := checkNote(n); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.UpdateNote(ctx, uid, n, cardOrds(n.Kind, n.Front)); err != nil {
		s.fail(w, err, "update note")
		return
	}
	full, err := s.store.GetNote(ctx, uid, n.ID)
	if err != nil {
		s.fail(w, err, "note")
		return
	}
	writeJSON(w, http.StatusOK, full)
}

func (s *Server) handleDeleteNote(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	n, err := s.store.GetNote(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "note")
		return
	}
	if app := linkApp(ctx); app != "" && n.Source != app {
		writeErr(w, http.StatusForbidden, "this card was not added by "+app)
		return
	}
	if err := s.store.DeleteNote(ctx, uid, n.ID); err != nil {
		s.fail(w, err, "delete note")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListNotes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	st, _ := s.store.GetSettings(ctx, uid)
	ck := s.clockFor(st)
	q := r.URL.Query()
	f := store.NoteFilter{DeckID: int64(queryInt(r, "deck")), Query: q.Get("q"), Tag: q.Get("tag"), State: q.Get("state"), Source: q.Get("source"),
		Sort: q.Get("sort"), Limit: queryInt(r, "limit"), Offset: queryInt(r, "offset"), DayEnd: store.TS(ck.dayEnd()), Today: ck.today()}
	notes, total, err := s.store.ListNotes(ctx, uid, f)
	if err != nil {
		s.fail(w, err, "notes")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"notes": notes, "total": total})
}

func (s *Server) handleTags(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	t, err := s.store.Tags(ctx, actorID(ctx))
	if err != nil {
		s.fail(w, err, "tags")
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// handleBulkNotes changes many notes at once from the card browser.
func (s *Server) handleBulkNotes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in struct {
		IDs    []int64 `json:"ids"`
		Action string  `json:"action"` // move | tag | untag | suspend | unsuspend | forget | delete
		DeckID int64   `json:"deck_id"`
		Tag    string  `json:"tag"`
	}
	if err := decode(r, &in); err != nil || len(in.IDs) == 0 || len(in.IDs) > 5000 {
		writeErr(w, http.StatusBadRequest, "choose up to 5,000 cards")
		return
	}
	var n int
	var err error
	switch in.Action {
	case "move":
		if _, e := s.store.GetDeck(ctx, uid, in.DeckID); e != nil {
			writeErr(w, http.StatusBadRequest, "no such deck")
			return
		}
		n, err = s.store.MoveNotes(ctx, uid, in.IDs, in.DeckID)
	case "tag", "untag":
		n, err = s.store.TagNotes(ctx, uid, in.IDs, in.Tag, in.Action == "tag")
	case "suspend", "unsuspend", "forget":
		var cards []int64
		if cards, err = s.store.CardIDsOfNotes(ctx, uid, in.IDs); err == nil {
			if in.Action == "forget" {
				n, err = s.store.ForgetCards(ctx, uid, cards)
			} else {
				n, err = s.store.SuspendCards(ctx, uid, cards, in.Action == "suspend")
			}
		}
	case "delete":
		n, err = s.store.DeleteNotes(ctx, uid, in.IDs)
	default:
		writeErr(w, http.StatusBadRequest, "unknown action")
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "changed": n})
}

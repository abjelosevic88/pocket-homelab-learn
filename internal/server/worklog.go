package server

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-learn/internal/store"
)

// ---- the worklog contract (docs/worklog.md in pocket-homelab-time) ----
//
// Pocket Time reads the study sessions Learn kept itself (worklog:read) and
// writes the time it tracked for a deck back as a session (worklog:write,
// external_id "time:<entry id>", source "time"). Refs are "deck:<id>", or
// "deck:all" for studying all decks.

type worklogEntry struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	StartedAt  string `json:"started_at,omitempty"`
	EndedAt    string `json:"ended_at,omitempty"`
	Date       string `json:"date"`
	DurationS  int    `json:"duration_s"`
	Ref        string `json:"ref"`
	Label      string `json:"label"`
	URL        string `json:"url"`
	Source     string `json:"source"`
	ExternalID string `json:"external_id"`
}

func sessionRef(x *store.StudySession) (ref, label, url string) {
	if x.DeckID == nil {
		if x.DeckName != "" {
			return "deck:all", x.DeckName, "/study" // its deck was deleted
		}
		return "deck:all", "All decks", "/study"
	}
	return fmt.Sprintf("deck:%d", *x.DeckID), x.DeckName, fmt.Sprintf("/decks/%d", *x.DeckID)
}

func worklogOf(x *store.StudySession) worklogEntry {
	ref, label, url := sessionRef(x)
	title := x.Title
	if title == "" {
		title = "Studying " + label
		if x.DeckID == nil && x.DeckName == "" {
			title = "Studying"
		}
	}
	return worklogEntry{ID: strconv.FormatInt(x.ID, 10), Title: title, StartedAt: x.StartedAt, EndedAt: x.EndedAt, Date: x.Date, DurationS: x.DurationS,
		Ref: ref, Label: label, URL: url, Source: x.Source, ExternalID: x.ExternalID}
}

// handleWorklog is the person's study sessions in a period (local dates, inclusive).
func (s *Server) handleWorklog(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	from, to := q.Get("from"), q.Get("to")
	if !validDate(from) || !validDate(to) {
		writeErr(w, http.StatusBadRequest, "from and to are dates (YYYY-MM-DD)")
		return
	}
	if to == "" {
		to = store.LocalDate(s.now())
	}
	if from == "" {
		from = store.AddDays(to, -89)
	}
	list, err := s.store.ListStudySessions(ctx, actorID(ctx), store.SessionFilter{From: from, To: to, Own: true})
	if err != nil {
		s.fail(w, err, "worklog")
		return
	}
	out := make([]worklogEntry, 0, len(list))
	for i := range list {
		if list[i].DurationS > 0 {
			out = append(out, worklogOf(&list[i]))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": out})
}

type worklogItem struct {
	Ref    string `json:"ref"`
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
	URL    string `json:"url"`
}

// handleWorklogItems is what study time can be for: the decks, the ones
// studied lately first (and "All decks").
func (s *Server) handleWorklogItems(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	st, _ := s.store.GetSettings(ctx, uid)
	decks, _, err := s.decksWithCounts(ctx, uid, st, false)
	if err != nil {
		s.fail(w, err, "decks")
		return
	}
	last, _ := s.store.DeckActivity(ctx, uid)
	sort.SliceStable(decks, func(i, j int) bool { return last[decks[i].ID] > last[decks[j].ID] })
	out := []worklogItem{}
	if q == "" || strings.Contains("all decks studying", q) {
		out = append(out, worklogItem{Ref: "deck:all", Label: "All decks", Detail: "Studying everything that is due", URL: "/study"})
	}
	for _, d := range decks {
		if q != "" && !strings.Contains(strings.ToLower(d.Name), q) {
			continue
		}
		c := d.Counts
		detail := fmt.Sprintf("%d cards", c.Total)
		if due := c.DueNew + c.DueLearn + c.DueReview; due > 0 {
			detail = fmt.Sprintf("%d due · %d cards", due, c.Total)
		}
		out = append(out, worklogItem{Ref: fmt.Sprintf("deck:%d", d.ID), Label: d.Name, Detail: detail, URL: fmt.Sprintf("/decks/%d", d.ID)})
		if len(out) >= 50 {
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

// worklogSource is who is writing: the acting app on a Core link; with a
// person's own token, "time" for what carries Time's external id, else "api".
func worklogSource(r *http.Request, external string) string {
	if a := linkApp(r.Context()); a != "" {
		return a
	}
	if strings.HasPrefix(external, "time:") {
		return "time"
	}
	return "api"
}

// handleWorklogLog records study time for a deck, idempotent on external_id
// (the same id again updates that record). Time tracked in Pocket Time for a
// run started here lands in that run's row (same timer), so it counts once.
func (s *Server) handleWorklogLog(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in struct {
		Ref        string `json:"ref"`
		Title      string `json:"title"`
		StartedAt  string `json:"started_at"`
		EndedAt    string `json:"ended_at"`
		Date       string `json:"date"`
		DurationS  int    `json:"duration_s"`
		ExternalID string `json:"external_id"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	ext := strings.TrimSpace(in.ExternalID)
	if len(ext) > 200 {
		writeErr(w, http.StatusBadRequest, "external_id is too long")
		return
	}
	src := worklogSource(r, ext)
	// what it was for
	var deck *store.Deck
	switch ref := strings.TrimSpace(in.Ref); {
	case ref == "" || ref == "deck:all":
	case strings.HasPrefix(ref, "deck:"):
		id, err := strconv.ParseInt(strings.TrimPrefix(ref, "deck:"), 10, 64)
		if err == nil {
			deck, err = s.store.GetDeck(ctx, uid, id)
		}
		if err != nil || deck == nil {
			writeErr(w, http.StatusBadRequest, "there is no such deck in Pocket Learn ("+ref+")")
			return
		}
	default:
		writeErr(w, http.StatusBadRequest, "Pocket Learn logs time for a deck: ref is deck:<id> or deck:all")
		return
	}
	// when, and how long
	start, okStart := store.ParseStamp(in.StartedAt)
	end, okEnd := store.ParseStamp(in.EndedAt)
	dur := in.DurationS
	if dur <= 0 && okStart && okEnd {
		dur = int(end.Sub(start).Seconds())
	}
	if dur < 0 || dur > 24*3600 || (dur == 0 && ext == "") { // a timer stopped at once still arrives (and stays out of the worklog)
		writeErr(w, http.StatusBadRequest, "duration_s is the time studied in seconds (up to a day)")
		return
	}
	date := strings.TrimSpace(in.Date)
	if date != "" && !validDate(date) {
		writeErr(w, http.StatusBadRequest, "date is YYYY-MM-DD")
		return
	}
	if !okStart {
		if okEnd {
			start, okStart = end.Add(-time.Duration(dur)*time.Second), true
		} else if date != "" {
			start, _ = time.ParseInLocation("2006-01-02 15:04", date+" 12:00", time.Local)
		} else {
			writeErr(w, http.StatusBadRequest, "started_at or date is needed")
			return
		}
	}
	if !okEnd {
		end = start.Add(time.Duration(dur) * time.Second)
	}
	if date == "" {
		date = store.LocalDate(start)
	}

	// the record: the same external id again, else the run its timer timed
	var x *store.StudySession
	if ext != "" {
		if cur, err := s.store.SessionByExternal(ctx, uid, ext); err == nil {
			if cur.Source != src {
				writeErr(w, http.StatusConflict, "that external_id belongs to another app's record")
				return
			}
			x = cur
		}
	}
	if x == nil && src == "time" && strings.HasPrefix(ext, "time:") {
		if n, err := strconv.ParseInt(strings.TrimPrefix(ext, "time:"), 10, 64); err == nil && n > 0 {
			if cur, err := s.store.SessionByTimer(ctx, uid, n); err == nil && cur.Source == "app" && cur.ExternalID == "" {
				x = cur
			}
		}
	}
	created := x == nil
	if created {
		x = &store.StudySession{}
	}
	x.Source, x.ExternalID = src, ext
	x.DeckID, x.DeckName = nil, ""
	if deck != nil {
		x.DeckID, x.DeckName = &deck.ID, deck.Name
	}
	x.Title = clip(strings.TrimSpace(in.Title), 300)
	if deck != nil && (x.Title == deck.Name || x.Title == "Studying "+deck.Name) {
		x.Title = "" // the deck says it already
	}
	x.StartedAt, x.EndedAt, x.LastAt = store.Stamp(start), store.Stamp(end), store.Stamp(end)
	x.Date, x.DurationS = date, dur
	var err error
	if created {
		err = s.store.CreateStudySession(ctx, uid, x)
	} else {
		err = s.store.SaveStudySession(ctx, uid, x)
	}
	if err != nil {
		s.fail(w, err, "log time")
		return
	}
	_, _, url := sessionRef(x)
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, map[string]any{"id": strconv.FormatInt(x.ID, 10), "url": url})
}

// handleWorklogDelete removes a record the caller made through the API
// (on a Core link: only that app's); Learn's own sessions stay.
func (s *Server) handleWorklogDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	id, err := strconv.ParseInt(chiParam(r, "id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	x, err := s.store.GetStudySession(ctx, uid, id)
	if err != nil {
		s.fail(w, err, "worklog")
		return
	}
	app := linkApp(ctx)
	if x.Source == "app" || (app != "" && x.Source != app) {
		writeErr(w, http.StatusForbidden, "only records that app logged itself can be removed")
		return
	}
	if err := s.store.DeleteStudySession(ctx, uid, id); err != nil {
		s.fail(w, err, "worklog")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

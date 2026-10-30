package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/abjelosevic88/pocket-homelab-learn/internal/pocket"
	"github.com/abjelosevic88/pocket-homelab-learn/internal/store"
)

// a deck with one card for Alice; returns the deck id and the card id.
func (a *app) deckWithCard(name string) (float64, float64) {
	a.t.Helper()
	n := a.expect(201, a.alice, "POST", "/api/v1/notes", map[string]any{"deck": name, "front": "Capital of Peru?", "back": "Lima"})
	note := n["note"].(map[string]any)
	return num(note, "deck_id"), num(note["cards"].([]any)[0].(map[string]any), "id")
}

func sessionOf(m map[string]any) map[string]any {
	x, _ := m["session"].(map[string]any)
	return x
}

// Without Pocket Time Learn records its own study sessions, per person.
func TestStudySessionsOwn(t *testing.T) {
	a := newApp(t, nil)
	deck, card := a.deckWithCard("Capitals")
	st := a.ok(a.alice, "POST", "/api/v1/study/start", map[string]any{"deck": deck})
	x := sessionOf(st)
	if x == nil || st["time"] != false || num(x, "timer_entry_id") != 0 {
		t.Fatalf("start = %v", st)
	}
	sid := num(x, "id")
	// starting again (a reload) picks the same run up
	if again := sessionOf(a.ok(a.alice, "POST", "/api/v1/study/start", map[string]any{"deck": deck})); num(again, "id") != sid {
		t.Fatalf("restart made a second session: %v", again)
	}
	r := a.ok(a.alice, "POST", "/api/v1/study/answer", map[string]any{"card_id": card, "rating": 1, "deck": deck, "session_id": sid})
	if s := sessionOf(r); num(s, "cards") != 1 || num(s, "again") != 1 {
		t.Fatalf("answer session = %v", s)
	}
	// Bob cannot see, stop or poll Alice's run
	a.expect(404, a.bob, "POST", "/api/v1/study/stop", map[string]any{"session_id": sid})
	a.expect(404, a.bob, "GET", fmt.Sprintf("/api/v1/time/timer?session=%.0f", sid), nil)
	a.expect(404, a.bob, "DELETE", fmt.Sprintf("/api/v1/worklog/%.0f", sid), nil)
	f := a.ok(a.alice, "POST", "/api/v1/study/finish", map[string]any{"deck": deck, "cards": 1, "session_id": sid})
	if s := sessionOf(f); s["ended_at"] == "" || s["source"] != "app" {
		t.Fatalf("finished session = %v", s)
	}
	// make it last five minutes, so the worklog has time to show
	x2, _ := a.st.GetStudySession(context.Background(), a.ids["Alice"], int64(sid))
	start, _ := store.ParseStamp(x2.StartedAt)
	x2.EndedAt, x2.DurationS = store.Stamp(start.Add(5*time.Minute)), 300
	if err := a.st.SaveStudySession(context.Background(), a.ids["Alice"], x2); err != nil {
		t.Fatal(err)
	}
	wl := a.ok(a.alice, "GET", "/api/v1/worklog", nil)["entries"].([]any)
	if len(wl) != 1 {
		t.Fatalf("worklog = %v", wl)
	}
	e := wl[0].(map[string]any)
	if e["ref"] != fmt.Sprintf("deck:%.0f", deck) || e["url"] != fmt.Sprintf("/decks/%.0f", deck) || e["source"] != "app" || num(e, "duration_s") != 300 || e["title"] != "Studying Capitals" {
		t.Fatalf("entry = %v", e)
	}
	if b := a.ok(a.bob, "GET", "/api/v1/worklog", nil)["entries"].([]any); len(b) != 0 {
		t.Fatalf("bob sees %v", b)
	}
	// Learn's own sessions are not the API's to remove
	a.expect(403, a.alice, "DELETE", fmt.Sprintf("/api/v1/worklog/%.0f", sid), nil)
	if st := a.ok(a.alice, "GET", "/api/v1/stats?range=w", nil); num(st["session_totals"].(map[string]any), "count") != 1 {
		t.Fatalf("stats sessions = %v", st["session_totals"])
	}
	// a run with no answer is not a session
	empty := sessionOf(a.ok(a.alice, "POST", "/api/v1/study/start", map[string]any{"deck": deck}))
	a.ok(a.alice, "POST", "/api/v1/study/stop", map[string]any{"session_id": num(empty, "id")})
	if l, _ := a.st.ListStudySessions(context.Background(), a.ids["Alice"], store.SessionFilter{}); len(l) != 1 {
		t.Fatalf("%d sessions, want 1", len(l))
	}
	a.expect(404, a.bob, "POST", "/api/v1/study/start", map[string]any{"deck": deck})
}

// A run nobody answered in for 15 minutes ends at its last answer.
func TestStudySessionIdle(t *testing.T) {
	a := newApp(t, nil)
	ctx := context.Background()
	uid := a.ids["Alice"]
	start := time.Now().Add(-40 * time.Minute)
	last := start.Add(6 * time.Minute)
	x := &store.StudySession{StartedAt: store.Stamp(start), LastAt: store.Stamp(last), Date: store.LocalDate(start), Cards: 12, Source: "app"}
	if err := a.st.CreateStudySession(ctx, uid, x); err != nil {
		t.Fatal(err)
	}
	a.s.closeIdleSessions(ctx)
	got, _ := a.st.GetStudySession(ctx, uid, x.ID)
	if got.Open() || got.DurationS != 360 {
		t.Fatalf("idle session = %+v", got)
	}
}

func TestWorklogLog(t *testing.T) {
	a := newApp(t, nil)
	deck, _ := a.deckWithCard("Spanish")
	ref := fmt.Sprintf("deck:%.0f", deck)
	body := map[string]any{"ref": ref, "title": "Spanish", "started_at": "2026-10-28T09:10:00+01:00", "ended_at": "2026-10-28T09:40:00+01:00", "date": "2026-10-28", "duration_s": 1800, "external_id": "time:812"}
	first := a.expect(201, a.alice, "POST", "/api/v1/worklog", body)
	if first["url"] != fmt.Sprintf("/decks/%.0f", deck) {
		t.Fatalf("answer = %v", first)
	}
	// an edited entry is sent again: the same record changes
	body["duration_s"] = 2400
	if again := a.ok(a.alice, "POST", "/api/v1/worklog", body); again["id"] != first["id"] {
		t.Fatalf("not idempotent: %v vs %v", again, first)
	}
	wl := a.ok(a.alice, "GET", "/api/v1/worklog?from=2026-10-28&to=2026-10-28", nil)["entries"].([]any)
	if len(wl) != 1 {
		t.Fatalf("worklog = %v", wl)
	}
	if e := wl[0].(map[string]any); num(e, "duration_s") != 2400 || e["source"] != "time" || e["external_id"] != "time:812" || e["ref"] != ref {
		t.Fatalf("entry = %v", e)
	}
	if l := a.ok(a.alice, "GET", "/api/v1/worklog?from=2026-10-29&to=2026-10-30", nil)["entries"].([]any); len(l) != 0 {
		t.Fatalf("outside the period: %v", l)
	}
	a.expect(400, a.alice, "POST", "/api/v1/worklog", map[string]any{"ref": "deck:999999", "duration_s": 60, "date": "2026-10-28", "external_id": "time:1"})
	a.expect(400, a.alice, "POST", "/api/v1/worklog", map[string]any{"ref": "issue:ATH-1", "duration_s": 60, "date": "2026-10-28"})
	a.expect(400, a.alice, "POST", "/api/v1/worklog", map[string]any{"ref": ref, "date": "2026-10-28"})
	// Bob cannot log time to Alice's deck, nor see or remove her record
	a.expect(400, a.bob, "POST", "/api/v1/worklog", body)
	a.expect(404, a.bob, "DELETE", "/api/v1/worklog/"+first["id"].(string), nil)
	// all decks
	a.expect(201, a.alice, "POST", "/api/v1/worklog", map[string]any{"ref": "deck:all", "duration_s": 600, "date": "2026-10-27", "external_id": "time:813"})
	items := a.ok(a.alice, "GET", "/api/v1/worklog/items", nil)["items"].([]any)
	if len(items) != 2 || items[0].(map[string]any)["ref"] != "deck:all" || items[1].(map[string]any)["ref"] != ref {
		t.Fatalf("items = %v", items)
	}
	if found := a.ok(a.alice, "GET", "/api/v1/worklog/items?q=span", nil)["items"].([]any); len(found) != 1 {
		t.Fatalf("q = %v", found)
	}
	if b := a.ok(a.bob, "GET", "/api/v1/worklog/items", nil)["items"].([]any); len(b) != 1 {
		t.Fatalf("bob's items = %v", b)
	}
	a.expect(204, a.alice, "DELETE", "/api/v1/worklog/"+first["id"].(string), nil)
	a.expect(404, a.alice, "DELETE", "/api/v1/worklog/"+first["id"].(string), nil)
}

// fakeTime is the part of Pocket Time's timer API Learn uses.
type fakeTime struct {
	mu      sync.Mutex
	next    int64
	running *pocket.TimeEntry
	starts  []map[string]any
	stops   []map[string]any
}

func (f *fakeTime) server(t *testing.T) *httptest.Server {
	write := func(w http.ResponseWriter, code int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(v)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		write(w, 200, map[string]any{"status": "ok", "version": "0.1.0"})
	})
	mux.HandleFunc("GET /api/v1/auth/me", func(w http.ResponseWriter, r *http.Request) {
		write(w, 200, map[string]any{"email": "alice@example.com"})
	})
	mux.HandleFunc("GET /api/v1/timer", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		write(w, 200, map[string]any{"running": f.running})
	})
	mux.HandleFunc("POST /api/v1/timer/start", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.starts = append(f.starts, in)
		if f.running != nil && in["switch"] != true {
			write(w, 409, map[string]any{"error": "a timer is already running: " + f.running.Name(), "running": f.running})
			return
		}
		f.next++
		f.running = &pocket.TimeEntry{ID: f.next, Running: true, Source: "learn", App: "learn", AppRef: in["app_ref"].(string), AppLabel: in["app_label"].(string),
			AppURL: in["app_url"].(string), StartedAt: time.Now().Format(time.RFC3339)}
		write(w, 200, map[string]any{"running": f.running})
	})
	mux.HandleFunc("POST /api/v1/timer/stop", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.stops = append(f.stops, in)
		if f.running == nil {
			write(w, 200, map[string]any{"running": nil, "stopped": nil})
			return
		}
		if id, ok := in["only_if"].(float64); ok && int64(id) != f.running.ID {
			write(w, 409, map[string]any{"error": "a different timer is running now", "running": f.running})
			return
		}
		stopped := f.running
		f.running = nil
		write(w, 200, map[string]any{"running": nil, "stopped": stopped})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

// With Pocket Time connected the study timer is Time's timer: one run, one
// record (Time's, written back into the run's row), never two.
func TestStudyTimerInPocketTime(t *testing.T) {
	a := newApp(t, nil)
	ft := &fakeTime{}
	ts := ft.server(t)
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"time_mode": "direct", "time_url": ts.URL, "time_token": "pt_test"})
	if test := a.ok(a.alice, "POST", "/api/v1/integrations/test", map[string]any{"which": "time"}); test["ok"] != true {
		t.Fatalf("test = %v", test)
	}
	deck, card := a.deckWithCard("Capitals")
	st := a.ok(a.alice, "POST", "/api/v1/study/start", map[string]any{"deck": deck})
	x := sessionOf(st)
	if st["time"] != true || num(x, "timer_entry_id") != 1 || len(ft.starts) != 1 {
		t.Fatalf("start = %v", st)
	}
	if s0 := ft.starts[0]; s0["app_ref"] != fmt.Sprintf("deck:%.0f", deck) || s0["app_label"] != "Capitals" || s0["app_url"] != fmt.Sprintf("/decks/%.0f", deck) || s0["deliver"] != true {
		t.Fatalf("timer start = %v", s0)
	}
	sid := num(x, "id")
	// reopening the study screen picks the running timer up, no prompt
	if again := a.ok(a.alice, "POST", "/api/v1/study/start", map[string]any{"deck": deck}); num(sessionOf(again), "id") != sid || again["conflict"] != nil {
		t.Fatalf("restart = %v", again)
	}
	tm := a.ok(a.alice, "GET", fmt.Sprintf("/api/v1/time/timer?session=%.0f", sid), nil)
	if tm["connected"] != true || tm["running"].(map[string]any)["mine"] != true {
		t.Fatalf("timer = %v", tm)
	}
	a.ok(a.alice, "POST", "/api/v1/study/answer", map[string]any{"card_id": card, "rating": 3, "deck": deck, "session_id": sid})
	a.ok(a.alice, "POST", "/api/v1/study/finish", map[string]any{"deck": deck, "cards": 1, "session_id": sid})
	if len(ft.stops) != 1 || ft.stops[0]["only_if"] != float64(1) || ft.running != nil {
		t.Fatalf("stops = %v", ft.stops)
	}
	// Time keeps the time: Learn offers nothing of its own for this run
	if wl := a.ok(a.alice, "GET", "/api/v1/worklog", nil)["entries"].([]any); len(wl) != 0 {
		t.Fatalf("worklog before Time wrote back = %v", wl)
	}
	// Time writes the finished time back: it lands in the same row
	now := time.Now()
	w := a.ok(a.alice, "POST", "/api/v1/worklog", map[string]any{"ref": fmt.Sprintf("deck:%.0f", deck), "title": "Capitals", "started_at": now.Add(-10 * time.Minute).Format(time.RFC3339),
		"ended_at": now.Format(time.RFC3339), "duration_s": 600, "external_id": "time:1"})
	if w["id"] != fmt.Sprintf("%.0f", sid) {
		t.Fatalf("written back as %v, want the run %.0f", w["id"], sid)
	}
	list, _ := a.st.ListStudySessions(context.Background(), a.ids["Alice"], store.SessionFilter{})
	if len(list) != 1 || list[0].Cards != 1 || list[0].Source != "time" || list[0].DurationS != 600 {
		t.Fatalf("sessions = %+v", list)
	}
	if wl := a.ok(a.alice, "GET", "/api/v1/worklog", nil)["entries"].([]any); len(wl) != 1 || wl[0].(map[string]any)["source"] != "time" {
		t.Fatalf("worklog = %v (Time skips source time)", wl)
	}

	// another timer runs (started in Pocket Guitar): offer to switch
	ft.mu.Lock()
	ft.next++
	ft.running = &pocket.TimeEntry{ID: ft.next, Running: true, Source: "guitar", App: "guitar", AppRef: "tab:42", AppLabel: "Wonderwall", StartedAt: now.Format(time.RFC3339)}
	ft.mu.Unlock()
	c := a.ok(a.alice, "POST", "/api/v1/study/start", map[string]any{"deck": deck})
	conf, _ := c["conflict"].(map[string]any)
	if c["session"] != nil || conf == nil || conf["name"] != "Wonderwall" || conf["started_in"] != "Pocket Guitar" {
		t.Fatalf("conflict = %v", c)
	}
	if tm := a.ok(a.alice, "GET", "/api/v1/time/timer", nil); tm["running"].(map[string]any)["mine"] != false {
		t.Fatalf("a timer from Guitar is not Learn's: %v", tm)
	}
	sw := a.ok(a.alice, "POST", "/api/v1/study/start", map[string]any{"deck": deck, "switch": true})
	x = sessionOf(sw)
	if x == nil || num(x, "timer_entry_id") != 3 || ft.running.ID != 3 {
		t.Fatalf("switch = %v", sw)
	}
	// stopped in Pocket Time: the run ends here too
	ft.mu.Lock()
	ft.running = nil
	ft.mu.Unlock()
	tm = a.ok(a.alice, "GET", fmt.Sprintf("/api/v1/time/timer?session=%.0f", num(x, "id")), nil)
	if tm["running"] != nil || sessionOf(tm)["ended_at"] == "" {
		t.Fatalf("after stop in Time = %v", tm)
	}
	// Bob has no Pocket Time connection of his own: Learn keeps his time
	if b := a.ok(a.bob, "GET", "/api/v1/time/timer", nil); b["connected"] != false {
		t.Fatalf("bob = %v", b)
	}
}

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/abjelosevic88/pocket-homelab-learn/internal/config"
	"github.com/abjelosevic88/pocket-homelab-learn/internal/store"
)

type app struct {
	t     *testing.T
	url   string
	s     *Server
	st    *store.Store
	alice string
	bob   string
	ids   map[string]int64
}

func newApp(t *testing.T, tweak func(*config.Config)) *app {
	t.Helper()
	cfg := testConfig(t.TempDir())
	cfg.BaseURL = "http://learn.test"
	cfg.MaxBodyBytes = 64 << 20
	if tweak != nil {
		tweak(cfg)
	}
	st := openTestStore(t, cfg)
	ctx := context.Background()
	a := &app{t: t, st: st, ids: map[string]int64{}}
	for _, u := range []*store.User{{Email: "alice@example.com", Name: "Alice", Role: "admin"}, {Email: "bob@example.com", Name: "Bob", Role: "member"}} {
		if err := st.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
		tok, _, _ := st.CreateAPIToken(ctx, u.ID, "t")
		a.ids[u.Name] = u.ID
		if u.Name == "Alice" {
			a.alice = tok
		} else {
			a.bob = tok
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	a.s = New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err := a.s.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(a.s.Router())
	t.Cleanup(ts.Close)
	a.url = ts.URL
	return a
}

func (a *app) call(auth, method, path string, body any) (int, map[string]any, []any) {
	a.t.Helper()
	var rd io.Reader
	if body != nil {
		j, _ := json.Marshal(body)
		rd = bytes.NewReader(j)
	}
	req, _ := http.NewRequest(method, a.url+path, rd)
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var m map[string]any
	var l []any
	if json.Unmarshal(raw, &m) != nil {
		_ = json.Unmarshal(raw, &l)
	}
	return res.StatusCode, m, l
}

func (a *app) ok(auth, method, path string, body any) map[string]any {
	a.t.Helper()
	code, m, _ := a.call(auth, method, path, body)
	if code >= 300 {
		a.t.Fatalf("%s %s → %d %v", method, path, code, m)
	}
	return m
}

func (a *app) expect(want int, auth, method, path string, body any) map[string]any {
	a.t.Helper()
	code, m, _ := a.call(auth, method, path, body)
	if code != want {
		a.t.Fatalf("%s %s → %d, want %d (%v)", method, path, code, want, m)
	}
	return m
}

func num(m map[string]any, k string) float64 { v, _ := m[k].(float64); return v }

func TestStreakOf(t *testing.T) {
	today := "2026-10-29"
	cases := []struct {
		days      []string
		cur, best int
	}{
		{nil, 0, 0},
		{[]string{"2026-10-29", "2026-10-28", "2026-10-27"}, 3, 3},
		{[]string{"2026-10-28", "2026-10-27"}, 2, 2},
		{[]string{"2026-10-26", "2026-10-25", "2026-10-24", "2026-10-23"}, 0, 4},
		{[]string{"2026-10-29", "2026-10-27", "2026-10-26"}, 1, 2},
	}
	for _, c := range cases {
		cur, best := streakOf(c.days, today)
		if cur != c.cur || best != c.best {
			t.Fatalf("streakOf(%v) = %d, %d; want %d, %d", c.days, cur, best, c.cur, c.best)
		}
	}
}

func TestCardOrds(t *testing.T) {
	if o := cardOrds("reverse", "x"); len(o) != 2 {
		t.Fatalf("reverse = %v", o)
	}
	if o := cardOrds("cloze", "{{c2::a}} and {{c1::b::hint}} and {{c2::c}}"); len(o) != 2 || o[0] != 1 || o[1] != 2 {
		t.Fatalf("cloze = %v", o)
	}
	if got := clozeText("{{c1::Paris}} is in {{c2::France::a country}}", 2, false); got != "Paris is in [a country]" {
		t.Fatalf("clozeText = %q", got)
	}
}

// One person's decks, cards and answers are theirs: another person gets 404.
func TestPerPersonIsolation(t *testing.T) {
	a := newApp(t, nil)
	d := a.expect(201, a.alice, "POST", "/api/v1/decks", map[string]any{"name": "Spanish"})
	did := num(d, "id")
	n := a.expect(201, a.alice, "POST", "/api/v1/notes", map[string]any{"deck_id": did, "kind": "reverse", "front": "la casa", "back": "the house"})
	nid := num(n["note"].(map[string]any), "id")
	cards := n["note"].(map[string]any)["cards"].([]any)
	if len(cards) != 2 {
		t.Fatalf("reverse made %d cards", len(cards))
	}
	cid := num(cards[0].(map[string]any), "id")
	for _, p := range []string{fmt.Sprintf("/api/v1/decks/%.0f", did), fmt.Sprintf("/api/v1/notes/%.0f", nid), fmt.Sprintf("/api/v1/cards/%.0f/history", cid)} {
		a.expect(404, a.bob, "GET", p, nil)
	}
	a.expect(404, a.bob, "PUT", fmt.Sprintf("/api/v1/notes/%.0f", nid), map[string]any{"front": "hacked"})
	a.expect(404, a.bob, "DELETE", fmt.Sprintf("/api/v1/decks/%.0f", did), nil)
	a.expect(404, a.bob, "POST", "/api/v1/study/answer", map[string]any{"card_id": cid, "rating": 3})
	a.expect(400, a.bob, "POST", "/api/v1/notes", map[string]any{"deck_id": did, "front": "x", "back": "y"})
	if _, _, l := a.call(a.bob, "GET", "/api/v1/decks", nil); len(l) != 0 {
		t.Fatalf("bob sees decks: %v", l)
	}
	if b := a.ok(a.bob, "GET", "/api/v1/notes", nil); num(b, "total") != 0 {
		t.Fatalf("bob sees notes: %v", b)
	}
	// the admin does not see Bob's cards either
	a.expect(201, a.bob, "POST", "/api/v1/notes", map[string]any{"deck": "Bob's", "front": "q", "back": "a"})
	if al := a.ok(a.alice, "GET", "/api/v1/notes", nil); num(al, "total") != 1 {
		t.Fatalf("alice sees %v notes", al["total"])
	}
}

// Study a new card through its learning steps into review, undo an answer.
func TestStudyFlow(t *testing.T) {
	a := newApp(t, nil)
	a.expect(201, a.alice, "POST", "/api/v1/notes", map[string]any{"deck": "Capitals", "front": "Capital of Peru?", "back": "Lima"})
	a.expect(201, a.alice, "POST", "/api/v1/notes", map[string]any{"deck": "Capitals", "kind": "cloze", "front": "{{c1::Quito}} is the capital of {{c2::Ecuador}}"})
	next := a.ok(a.alice, "GET", "/api/v1/study/next", nil)
	if c := next["counts"].(map[string]any); num(c, "new") != 3 {
		t.Fatalf("counts = %v", c)
	}
	card := next["card"].(map[string]any)
	if len(card["answers"].([]any)) != 4 {
		t.Fatalf("answers = %v", card["answers"])
	}
	id := num(card, "id")
	r := a.ok(a.alice, "POST", "/api/v1/study/answer", map[string]any{"card_id": id, "rating": 3, "duration_ms": 3000})
	if c := r["card"].(map[string]any); num(c, "state") != 1 {
		t.Fatalf("after Good a new card is learning: %v", c)
	}
	// undo puts it back, as new
	u := a.ok(a.alice, "POST", "/api/v1/study/undo", map[string]any{"review_id": r["review_id"]})
	if c := u["card"].(map[string]any); num(c, "id") != id || num(c, "state") != 0 {
		t.Fatalf("undo = %v", c)
	}
	a.expect(404, a.alice, "POST", "/api/v1/study/undo", map[string]any{"review_id": r["review_id"]}) // already taken back
	// Easy graduates it straight to review, days ahead
	r = a.ok(a.alice, "POST", "/api/v1/study/answer", map[string]any{"card_id": id, "rating": 4})
	if c := r["card"].(map[string]any); num(c, "state") != 2 || num(c, "interval") < 2 {
		t.Fatalf("after Easy: %v", c)
	}
	d := a.ok(a.alice, "GET", "/api/v1/dashboard", nil)
	if num(d, "done_today") != 1 || num(d, "streak") != 1 {
		t.Fatalf("dashboard = %v", d)
	}
	day := a.ok(a.alice, "GET", "/api/v1/day/"+d["today"].(string), nil)
	if num(day, "reviews") != 1 {
		t.Fatalf("day = %v", day)
	}
	st := a.ok(a.alice, "GET", "/api/v1/stats?range=w", nil)
	if num(st["totals"].(map[string]any), "reviews") != 1 {
		t.Fatalf("stats = %v", st["totals"])
	}
	a.expect(400, a.alice, "POST", "/api/v1/notes", map[string]any{"deck": "Capitals", "kind": "cloze", "front": "no gap here"})
}

func TestLinkScopes(t *testing.T) {
	get := func(p string) *http.Request { return httptest.NewRequest("GET", p, nil) }
	post := func(p string) *http.Request { return httptest.NewRequest("POST", p, nil) }
	read, write := []string{"learn:read"}, []string{"learn:write"}
	if !linkAllows(read, get("/api/v1/day/2026-10-29")) || !linkAllows(read, get("/api/v1/dashboard")) || !linkAllows(read, get("/api/v1/decks")) {
		t.Fatal("learn:read should read the day, dashboard and decks")
	}
	if linkAllows(read, get("/api/v1/notes")) || linkAllows(read, post("/api/v1/notes")) || linkAllows(read, get("/api/v1/export.json")) || linkAllows(read, get("/api/v1/settings")) {
		t.Fatal("learn:read must not see cards, settings or the export")
	}
	if !linkAllows(write, post("/api/v1/notes")) || !linkAllows(write, get("/api/v1/decks")) || linkAllows(write, get("/api/v1/stats")) || linkAllows(write, post("/api/v1/study/answer")) {
		t.Fatal("learn:write adds cards and lists decks, nothing else")
	}
}

func TestExportImportIsIdempotent(t *testing.T) {
	a := newApp(t, nil)
	n := a.expect(201, a.alice, "POST", "/api/v1/notes", map[string]any{"deck": "Words", "kind": "reverse", "front": "perro", "back": "dog", "tags": "animals"})
	cid := num(n["note"].(map[string]any)["cards"].([]any)[0].(map[string]any), "id")
	a.ok(a.alice, "POST", "/api/v1/study/answer", map[string]any{"card_id": cid, "rating": 4})
	_, doc, _ := a.call(a.alice, "GET", "/api/v1/export.json", nil)
	r1 := a.ok(a.bob, "POST", "/api/v1/import", doc)
	if num(r1, "notes") != 1 || num(r1, "decks") != 1 {
		t.Fatalf("import = %v", r1)
	}
	r2 := a.ok(a.bob, "POST", "/api/v1/import", doc)
	if num(r2, "notes") != 0 || num(r2, "already") != 1 {
		t.Fatalf("second import = %v", r2)
	}
	if st := a.ok(a.bob, "GET", "/api/v1/stats?range=w", nil); num(st["totals"].(map[string]any), "reviews") != 1 {
		t.Fatalf("history not imported: %v", st["totals"])
	}
	// CSV rows, twice
	csv := map[string]any{"deck": "Quizlet", "text": "gato\tcat\ncasa\thouse\n\tempty"}
	if r := a.ok(a.alice, "POST", "/api/v1/import/csv", csv); num(r, "notes") != 2 || num(r, "skipped") != 1 {
		t.Fatalf("csv = %v", r)
	}
	if r := a.ok(a.alice, "POST", "/api/v1/import/csv", csv); num(r, "notes") != 0 || num(r, "already") != 2 {
		t.Fatalf("csv again = %v", r)
	}
}

func TestSettingsPerPerson(t *testing.T) {
	a := newApp(t, nil)
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"retention": 0.85, "reader_token": "prd_secretsecret", "title": "Family Learn", "learning_steps": "2m, 15m"})
	bob := a.ok(a.bob, "GET", "/api/v1/settings", nil)
	if num(bob, "retention") != 0.9 || bob["reader_token"] != "" || bob["title"] != "Family Learn" {
		t.Fatalf("bob's settings leak: %v", bob)
	}
	al := a.ok(a.alice, "GET", "/api/v1/settings", nil)
	if al["learning_steps"] != "2m 15m" || strings.Contains(al["reader_token"].(string), "secret") {
		t.Fatalf("alice settings = %v", al)
	}
	a.expect(400, a.alice, "PUT", "/api/v1/settings", map[string]any{"retention": 1.2})
	a.expect(400, a.alice, "PUT", "/api/v1/settings", map[string]any{"learning_steps": "soon"})
	a.expect(400, a.alice, "PUT", "/api/v1/settings", map[string]any{"reminder_time": "25:00"})
}

func TestWithinMinutes(t *testing.T) {
	if !withinMinutes("08:02", "08:00", 4) || withinMinutes("07:59", "08:00", 4) || withinMinutes("08:05", "08:00", 4) {
		t.Fatal("withinMinutes")
	}
}

func TestParseCards(t *testing.T) {
	c, err := parseCards("```json\n{\"cards\":[{\"kind\":\"basic\",\"front\":\"Q\",\"back\":\"A\"}]}\n```")
	if err != nil || len(c) != 1 || c[0].Back != "A" {
		t.Fatalf("parseCards = %v, %v", c, err)
	}
	if _, err := parseCards("sorry"); err == nil {
		t.Fatal("no cards should fail")
	}
}

var _ = time.Now
var _ = bytes.NewReader

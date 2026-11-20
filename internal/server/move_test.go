package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-learn/internal/config"
)

// moveLinks starts Learn behind a fake Core whose pcx_<who>_<export|import|read>
// tokens are Pocket Move links acting for alice@ or bob@.
func moveLinks(t *testing.T) *app {
	t.Helper()
	coreSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/auth/introspect" {
			io.WriteString(w, `{}`)
			return
		}
		var in struct {
			Token string `json:"token"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		parts := strings.SplitN(strings.TrimPrefix(in.Token, "pcx_"), "_", 2)
		if len(parts) != 2 {
			io.WriteString(w, `{"active":false}`)
			return
		}
		scope := map[string]string{"export": core.ScopeMoveExport, "import": core.ScopeMoveImport, "read": "learn:read"}[parts[1]]
		email := parts[0] + "@example.com"
		fmt.Fprintf(w, `{"active":true,"kind":"delegated","scope":%q,"audience":"learn","user":{"id":1,"sub":%q,"email":%q,"name":%q,"role":"member"},"app":{"id":7,"slug":"move","name":"Pocket Move"}}`, scope, email, email, parts[0])
	}))
	t.Cleanup(coreSrv.Close)
	return newApp(t, func(c *config.Config) { c.CoreURL = coreSrv.URL })
}

func (a *app) rawReq(tok, method, path, ctype string, body []byte) (int, []byte) {
	a.t.Helper()
	req, _ := http.NewRequest(method, a.url+path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b
}

func (a *app) moveImport(tok, path string, data []byte, multi bool) map[string]any {
	a.t.Helper()
	if !multi {
		code, out := a.rawReq(tok, "POST", path, "application/json", data)
		if code != 200 {
			a.t.Fatalf("POST %s → %d %s", path, code, out)
		}
		var m map[string]any
		_ = json.Unmarshal(out, &m)
		return m
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "upload")
	_, _ = fw.Write(data)
	_ = mw.Close()
	code, out := a.rawReq(tok, "POST", path, mw.FormDataContentType(), buf.Bytes())
	if code != 200 {
		a.t.Fatalf("POST %s → %d %s", path, code, out)
	}
	var m map[string]any
	_ = json.Unmarshal(out, &m)
	return m
}

func TestMoveLinkScopes(t *testing.T) {
	a := moveLinks(t)
	exp, imp, read := "pcx_bob_export", "pcx_bob_import", "pcx_bob_read"
	for _, c := range []struct {
		tok, method, path string
		want              int
	}{
		{exp, "GET", core.PortablePath, 200},
		{exp, "GET", "/api/v1/export.json", 200},
		{exp, "GET", "/api/v1/export.csv", 200},
		{exp, "POST", "/api/v1/import", 403},
		{exp, "POST", "/api/v1/import/anki", 403},
		{exp, "GET", "/api/v1/settings", 403},
		{exp, "GET", "/api/v1/notes", 403},
		{exp, "GET", "/api/v1/backup", 403},
		{imp, "GET", core.PortablePath, 200},
		{imp, "GET", "/api/v1/export.json", 403},
		{imp, "POST", "/api/v1/import/anki", 400}, // reachable; no .apkg sent
		{imp, "POST", "/api/v1/import/csv", 403},
		{imp, "PUT", "/api/v1/settings", 403},
		{imp, "POST", "/api/v1/notes", 403},
		{read, "GET", core.PortablePath, 403},
		{read, "GET", "/api/v1/export.json", 403},
	} {
		if code, body := a.rawReq(c.tok, c.method, c.path, "application/json", []byte("{}")); code != c.want {
			t.Errorf("%s %s %s → %d, want %d (%s)", c.tok, c.method, c.path, code, c.want, body)
		}
	}
	_, body := a.rawReq(exp, "GET", core.PortablePath, "", nil)
	var p core.Portable
	if err := json.Unmarshal(body, &p); err != nil || len(p.Exports) != 2 || len(p.Imports) != 2 || p.Exports[0].Kind != core.KindPocket || p.Imports[1].Form != "file" {
		t.Fatalf("portable: %s", body)
	}
}

// Move brings Alice's cards to Bob: the import adds once, and nothing of
// one person shows up for the other.
func TestMoveImportsAddOnce(t *testing.T) {
	a := moveLinks(t)
	n := a.expect(201, a.alice, "POST", "/api/v1/notes", map[string]any{"deck": "Words", "kind": "reverse", "front": "perro", "back": "dog"})
	cid := num(n["note"].(map[string]any)["cards"].([]any)[0].(map[string]any), "id")
	a.ok(a.alice, "POST", "/api/v1/study/answer", map[string]any{"card_id": cid, "rating": 4})

	code, export := a.rawReq("pcx_alice_export", "GET", "/api/v1/export.json", "", nil)
	if code != 200 || !strings.Contains(string(export), "perro") {
		t.Fatalf("alice's export: %d %s", code, export)
	}
	if code, csv := a.rawReq("pcx_alice_export", "GET", "/api/v1/export.csv", "", nil); code != 200 || !strings.Contains(string(csv), "perro") {
		t.Fatalf("alice's csv: %d %s", code, csv)
	}
	if _, bob := a.rawReq("pcx_bob_export", "GET", "/api/v1/export.json", "", nil); strings.Contains(string(bob), "perro") {
		t.Fatalf("bob's export holds alice's cards: %s", bob)
	}

	first := a.moveImport("pcx_bob_import", "/api/v1/import", export, false)
	if num(first, "notes") != 1 || num(first, "decks") != 1 {
		t.Fatalf("first import: %v", first)
	}
	second := a.moveImport("pcx_bob_import", "/api/v1/import", export, false)
	if num(second, "notes")+num(second, "decks") != 0 || num(second, "already") != 1 {
		t.Fatalf("second import added: %v", second)
	}
	if l := a.ok(a.bob, "GET", "/api/v1/notes", nil)["notes"].([]any); len(l) != 1 {
		t.Fatalf("bob's notes: %v", l)
	}
	if l := a.ok(a.alice, "GET", "/api/v1/notes", nil)["notes"].([]any); len(l) != 1 {
		t.Fatalf("alice's notes changed: %v", l)
	}
}

package anki

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

const crt = 1700000000 // the collection's creation day (unix seconds)

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

const tables = `
CREATE TABLE notes (id INTEGER PRIMARY KEY, guid TEXT, mid INTEGER, mod INTEGER, usn INTEGER, tags TEXT, flds TEXT, sfld TEXT, csum INTEGER, flags INTEGER, data TEXT);
CREATE TABLE cards (id INTEGER PRIMARY KEY, nid INTEGER, did INTEGER, ord INTEGER, mod INTEGER, usn INTEGER, type INTEGER, queue INTEGER, due INTEGER, ivl INTEGER, factor INTEGER, reps INTEGER, lapses INTEGER, left INTEGER, odue INTEGER, odid INTEGER, flags INTEGER, data TEXT);`

// fillNotes adds the same five notes to either schema: basic, reverse (two
// cards), cloze, type-in, and a note without cards.
func fillNotes(t *testing.T, db *sql.DB) {
	note := func(id int64, guid string, mid int64, tags string, flds ...string) {
		joined := ""
		for i, f := range flds {
			if i > 0 {
				joined += "\x1f"
			}
			joined += f
		}
		mustExec(t, db, `INSERT INTO notes (id, guid, mid, mod, usn, tags, flds, sfld, csum, flags, data) VALUES (?, ?, ?, 0, 0, ?, ?, '', 0, 0, '')`, id, guid, mid, tags, joined)
	}
	card := func(id, nid, did int64, ord, typ, queue int, due int64, ivl, factor, reps, lapses, flags int) {
		mustExec(t, db, `INSERT INTO cards (id, nid, did, ord, mod, usn, type, queue, due, ivl, factor, reps, lapses, left, odue, odid, flags, data) VALUES (?, ?, ?, ?, 0, 0, ?, ?, ?, ?, ?, ?, ?, 0, 0, 0, ?, '')`,
			id, nid, did, ord, typ, queue, due, ivl, factor, reps, lapses, flags)
	}
	note(1, "g-basic", 100, " french  verbs::er ", "<b>parler</b>", `to speak <img src="cat.jpg">`, "", "[sound:parler.mp3]")
	card(11, 1, 2, 0, 2, 2, 30, 12, 2500, 5, 1, 2)
	note(2, "g-rev", 101, "", "dog", "chien")
	card(21, 2, 2, 0, 0, 0, 1, 0, 0, 0, 0, 0)
	card(22, 2, 2, 1, 1, 1, crt+5000, 0, 2500, 1, 0, 0)
	note(3, "g-cloze", 102, "geo", "{{c1::Paris}} is the capital of {{c2::France}}", "Extra <i>info</i>")
	card(31, 3, 3, 0, 0, -1, 2, 0, 0, 0, 0, 0)
	card(32, 3, 3, 1, 0, 0, 3, 0, 0, 0, 0, 0)
	note(4, "g-type", 103, "", "Capital of Spain?", "Madrid")
	card(41, 4, 3, 0, 0, 0, 4, 0, 0, 0, 0, 0)
	note(5, "g-orphan", 100, "", "no", "cards")
}

func zipFile(t *testing.T, dir string, entries map[string][]byte) string {
	t.Helper()
	path := filepath.Join(dir, "deck.apkg")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, b := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(b); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return path
}

func newDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	return db
}

func schema11(t *testing.T, dir string) []byte {
	path := filepath.Join(dir, "c11.sqlite")
	db := newDB(t, path)
	mustExec(t, db, tables)
	mustExec(t, db, `CREATE TABLE col (id INTEGER PRIMARY KEY, crt INTEGER, models TEXT, decks TEXT)`)
	models := map[string]any{
		"100": map[string]any{"type": 0, "tmpls": []any{map[string]any{"ord": 0, "qfmt": "{{Front}}"}}},
		"101": map[string]any{"type": 0, "tmpls": []any{map[string]any{"ord": 0, "qfmt": "{{Front}}"}, map[string]any{"ord": 1, "qfmt": "{{Back}}"}}},
		"102": map[string]any{"type": 1, "tmpls": []any{map[string]any{"ord": 0, "qfmt": "{{cloze:Text}}"}}},
		"103": map[string]any{"type": 0, "tmpls": []any{map[string]any{"ord": 0, "qfmt": "{{Front}}\n{{type:Back}}"}}},
	}
	decks := map[string]any{"1": map[string]any{"name": "Default"}, "2": map[string]any{"name": "Languages::French"}, "3": map[string]any{"name": "Geography"}}
	mj, _ := json.Marshal(models)
	dj, _ := json.Marshal(decks)
	mustExec(t, db, `INSERT INTO col (id, crt, models, decks) VALUES (1, ?, ?, ?)`, crt, string(mj), string(dj))
	fillNotes(t, db)
	db.Close()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// protobuf helpers for the schema-18 fixtures
func pVarint(v uint64) []byte {
	var out []byte
	for v >= 0x80 {
		out = append(out, byte(v)|0x80)
		v >>= 7
	}
	return append(out, byte(v))
}
func pBytes(num int, b []byte) []byte {
	out := pVarint(uint64(num<<3 | 2))
	out = append(out, pVarint(uint64(len(b)))...)
	return append(out, b...)
}
func pUint(num int, v uint64) []byte { return append(pVarint(uint64(num<<3)), pVarint(v)...) }

func schema18(t *testing.T, dir string) []byte {
	path := filepath.Join(dir, "c18.sqlite")
	db := newDB(t, path)
	mustExec(t, db, tables)
	mustExec(t, db, `CREATE TABLE col (id INTEGER PRIMARY KEY, crt INTEGER, models TEXT, decks TEXT)`)
	mustExec(t, db, `INSERT INTO col (id, crt, models, decks) VALUES (1, ?, '', '')`, crt)
	mustExec(t, db, `CREATE TABLE notetypes (id INTEGER PRIMARY KEY, name TEXT, mtime_secs INTEGER, usn INTEGER, config BLOB)`)
	mustExec(t, db, `CREATE TABLE templates (ntid INTEGER, ord INTEGER, name TEXT, mtime_secs INTEGER, usn INTEGER, config BLOB)`)
	mustExec(t, db, `CREATE TABLE fields (ntid INTEGER, ord INTEGER, name TEXT, config BLOB)`)
	mustExec(t, db, `CREATE TABLE decks (id INTEGER PRIMARY KEY, name TEXT, mtime_secs INTEGER, usn INTEGER, common BLOB, kind BLOB)`)
	for id, kind := range map[int64]uint64{100: 0, 101: 0, 102: 1, 103: 0} {
		mustExec(t, db, `INSERT INTO notetypes VALUES (?, 'nt', 0, 0, ?)`, id, append(pUint(1, kind), pBytes(3, []byte("css"))...))
	}
	tmpl := func(ntid int64, ord int, q string) {
		mustExec(t, db, `INSERT INTO templates VALUES (?, ?, 't', 0, 0, ?)`, ntid, ord, append(pBytes(1, []byte(q)), pBytes(2, []byte("{{FrontSide}}"))...))
	}
	tmpl(100, 0, "{{Front}}")
	tmpl(101, 0, "{{Front}}")
	tmpl(101, 1, "{{Back}}")
	tmpl(102, 0, "{{cloze:Text}}")
	tmpl(103, 0, "{{Front}} {{type:Back}}")
	mustExec(t, db, `INSERT INTO decks VALUES (2, 'Languages`+"\x1f"+`French', 0, 0, x'', x''), (3, 'Geography', 0, 0, x'', x'')`)
	fillNotes(t, db)
	db.Close()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func zstdBytes(t *testing.T, b []byte) []byte {
	var buf bytes.Buffer
	w, err := zstd.NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	w.Write(b)
	w.Close()
	return buf.Bytes()
}

func checkPackage(t *testing.T, p *Package) {
	t.Helper()
	if len(p.Decks) != 2 || p.Decks[0].Name != "Geography" || p.Decks[1].Name != "Languages › French" || p.Decks[1].ID != 2 {
		t.Fatalf("decks %+v", p.Decks)
	}
	if !p.CollectionCreated.Equal(time.Unix(crt, 0)) {
		t.Fatalf("crt %v", p.CollectionCreated)
	}
	if len(p.Notes) != 4 {
		t.Fatalf("%d notes", len(p.Notes))
	}
	b, r, c, ty := p.Notes[0], p.Notes[1], p.Notes[2], p.Notes[3]
	if b.Kind != "basic" || b.Front != "**parler**" || b.Back != "to speak ![](media/cat.jpg)" || b.Extra != "[sound:parler.mp3]" || b.GUID != "g-basic" {
		t.Fatalf("basic %+v", b)
	}
	if len(b.Tags) != 2 || b.Tags[0] != "french" || b.Tags[1] != "verbs/er" {
		t.Fatalf("tags %v", b.Tags)
	}
	bc := b.Cards[0]
	if bc.Type != 2 || bc.Interval != 12 || bc.Factor != 2500 || bc.Reps != 5 || bc.Lapses != 1 || bc.Flag != 2 || bc.DeckID != 2 || !bc.Due.Equal(time.Unix(crt+30*86400, 0)) {
		t.Fatalf("review card %+v", bc)
	}
	if r.Kind != "reverse" || len(r.Cards) != 2 || r.Cards[1].Ord != 1 || !r.Cards[1].Due.Equal(time.Unix(crt+5000, 0)) || !r.Cards[0].Due.IsZero() {
		t.Fatalf("reverse %+v", r)
	}
	if c.Kind != "cloze" || c.Front != "{{c1::Paris}} is the capital of {{c2::France}}" || c.Back != "" || c.Extra != "Extra *info*" || c.Cards[0].Queue != -1 || len(c.Cards) != 2 {
		t.Fatalf("cloze %+v", c)
	}
	if ty.Kind != "type" || ty.Back != "Madrid" {
		t.Fatalf("type %+v", ty)
	}
	if len(p.Media) != 2 || p.Media[0] != "cat.jpg" || p.Media[1] != "parler.mp3" {
		t.Fatalf("media %v", p.Media)
	}
	for name, want := range map[string]string{"cat.jpg": "JPEGDATA", "parler.mp3": "MP3DATA"} {
		rc, err := p.OpenMedia(name)
		if err != nil {
			t.Fatal(name, err)
		}
		got, _ := io.ReadAll(rc)
		rc.Close()
		if string(got) != want {
			t.Fatalf("%s = %q", name, got)
		}
	}
	if _, err := p.OpenMedia("missing.png"); err == nil {
		t.Fatal("missing media opened")
	}
}

func TestOpenLegacy(t *testing.T) {
	dir := t.TempDir()
	media, _ := json.Marshal(map[string]string{"0": "cat.jpg", "1": "parler.mp3", "2": "unused.png"})
	path := zipFile(t, dir, map[string][]byte{"collection.anki2": schema11(t, dir), "media": media, "0": []byte("JPEGDATA"), "1": []byte("MP3DATA"), "2": []byte("PNG")})
	tmp := t.TempDir()
	p, err := Open(path, tmp)
	if err != nil {
		t.Fatal(err)
	}
	checkPackage(t, p)
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if left, _ := os.ReadDir(tmp); len(left) != 0 {
		t.Fatalf("temp files left: %v", left)
	}
}

func TestOpenAnki21b(t *testing.T) {
	dir := t.TempDir()
	entry := func(name string, legacy uint64) []byte {
		e := append(pBytes(1, []byte(name)), pUint(2, 8)...)
		e = append(e, pBytes(3, bytes.Repeat([]byte{1}, 20))...)
		if legacy > 0 {
			e = append(e, pUint(255, legacy)...)
		}
		return pBytes(1, e)
	}
	list := append(entry("cat.jpg", 0), entry("parler.mp3", 7)...)
	path := zipFile(t, dir, map[string][]byte{
		"collection.anki2":   []byte("dummy: please update Anki"),
		"collection.anki21b": zstdBytes(t, schema18(t, dir)),
		"media":              zstdBytes(t, list),
		"0":                  zstdBytes(t, []byte("JPEGDATA")),
		"7":                  zstdBytes(t, []byte("MP3DATA")),
	})
	p, err := Open(path, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	checkPackage(t, p)
}

func TestOpenNotAnki(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "x.apkg")
	os.WriteFile(bad, []byte("not a zip"), 0o644)
	if _, err := Open(bad, dir); err != ErrNotAnki {
		t.Fatalf("got %v", err)
	}
	empty := zipFile(t, dir, map[string][]byte{"readme.txt": []byte("hi")})
	if _, err := Open(empty, dir); err != ErrNotAnki {
		t.Fatalf("got %v", err)
	}
}

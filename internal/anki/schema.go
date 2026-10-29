package anki

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// notetype is what the reader needs to know about an Anki note type.
type notetype struct {
	cloze     bool
	templates int
	typeIn    bool // the front template asks to type the answer
}

var (
	clozeRe    = regexp.MustCompile(`\{\{c\d+::`)
	mediaRefRe = regexp.MustCompile(`!\[\]\(media/([^)]+)\)|\[sound:([^\]]+)\]`)
)

func deckName(n string) string {
	n = strings.ReplaceAll(n, "\x1f", "::")
	parts := strings.Split(n, "::")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return "Default"
	}
	return strings.Join(out, " › ")
}

func hasTable(db *sql.DB, name string) bool {
	var n int
	_ = db.QueryRow(`SELECT COUNT(1) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&n)
	return n > 0
}

// read loads decks, note types, notes and cards from the extracted collection.
func (p *Package) read() error {
	db, err := sql.Open("sqlite", "file:"+p.tmpFile+"?mode=ro")
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if !hasTable(db, "notes") || !hasTable(db, "cards") || !hasTable(db, "col") {
		return ErrNotAnki
	}
	var crt int64
	if err := db.QueryRow(`SELECT crt FROM col LIMIT 1`).Scan(&crt); err != nil {
		return fmt.Errorf("collection: %w", err)
	}
	p.CollectionCreated = time.Unix(crt, 0).UTC()

	var types map[int64]*notetype
	var decks map[int64]string
	schema18 := hasTable(db, "notetypes")
	if schema18 {
		types, decks, err = readSchema18(db)
	} else {
		types, decks, err = readSchema11(db)
	}
	if err != nil {
		return err
	}

	cards, err := readCards(db, crt)
	if err != nil {
		return err
	}

	rows, err := db.Query(`SELECT id, guid, mid, tags, flds FROM notes ORDER BY id`)
	if err != nil {
		return fmt.Errorf("notes: %w", err)
	}
	type rawNote struct {
		id, mid          int64
		guid, tags, flds string
	}
	var raws []rawNote
	for rows.Next() {
		var r rawNote
		if err := rows.Scan(&r.id, &r.guid, &r.mid, &r.tags, &r.flds); err != nil {
			rows.Close()
			return err
		}
		raws = append(raws, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	usedDecks := map[int64]bool{}
	usedMedia := map[string]bool{}
	for _, r := range raws {
		cs := cards[r.id]
		if len(cs) == 0 {
			continue
		}
		fields := strings.Split(r.flds, "\x1f")
		nt := types[r.mid]
		cloze := nt != nil && nt.cloze
		if nt == nil && clozeRe.MatchString(r.flds) {
			cloze = true
		}
		n := Note{GUID: r.guid, Tags: splitTags(r.tags), Cards: cs}
		conv := make([]string, len(fields))
		for i, f := range fields {
			conv[i] = HTMLToText(f)
		}
		extraFrom := 2
		switch {
		case cloze:
			n.Kind = "cloze"
			n.Front = conv[0]
			extraFrom = 1
		default:
			n.Kind = "basic"
			n.Front = conv[0]
			if len(conv) > 1 {
				n.Back = conv[1]
			}
			if nt != nil && nt.typeIn {
				n.Kind = "type"
			} else if nt != nil && nt.templates >= 2 {
				for _, c := range cs {
					if c.Ord >= 1 {
						n.Kind = "reverse"
						break
					}
				}
			}
		}
		var extra []string
		for i := extraFrom; i < len(conv); i++ {
			if conv[i] != "" {
				extra = append(extra, conv[i])
			}
		}
		n.Extra = strings.Join(extra, "\n\n")
		for _, c := range cs {
			usedDecks[c.DeckID] = true
		}
		for _, f := range []string{n.Front, n.Back, n.Extra} {
			for _, m := range mediaRefRe.FindAllStringSubmatch(f, -1) {
				name := m[1]
				if name == "" {
					name = m[2]
				}
				if _, ok := p.media[name]; ok {
					usedMedia[name] = true
				}
			}
		}
		p.Notes = append(p.Notes, n)
	}
	for id := range usedDecks {
		name, ok := decks[id]
		if !ok {
			name = "Default"
		}
		p.Decks = append(p.Decks, Deck{ID: id, Name: name})
	}
	sort.Slice(p.Decks, func(i, j int) bool {
		if p.Decks[i].Name != p.Decks[j].Name {
			return p.Decks[i].Name < p.Decks[j].Name
		}
		return p.Decks[i].ID < p.Decks[j].ID
	})
	p.Media = sortedNames(usedMedia)
	return nil
}

func splitTags(s string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, t := range strings.Fields(s) {
		t = strings.ReplaceAll(t, "::", "/")
		if t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// readCards groups the cards by note, with due dates made absolute.
func readCards(db *sql.DB, crt int64) (map[int64][]Card, error) {
	rows, err := db.Query(`SELECT nid, did, ord, type, queue, due, ivl, factor, reps, lapses, flags, odid, odue FROM cards ORDER BY nid, ord`)
	if err != nil {
		return nil, fmt.Errorf("cards: %w", err)
	}
	defer rows.Close()
	out := map[int64][]Card{}
	for rows.Next() {
		var nid, did, due, odid, odue int64
		var c Card
		if err := rows.Scan(&nid, &did, &c.Ord, &c.Type, &c.Queue, &due, &c.Interval, &c.Factor, &c.Reps, &c.Lapses, &c.Flag, &odid, &odue); err != nil {
			return nil, err
		}
		if odid != 0 { // in a filtered deck: the home deck and its own due
			did = odid
			if odue != 0 {
				due = odue
			}
		}
		c.DeckID = did
		c.Flag &= 7
		if c.Interval < 0 {
			c.Interval = 0
		}
		switch c.Type {
		case 0:
		case 1, 3:
			if due > 1_000_000_000 {
				c.Due = time.Unix(due, 0).UTC()
			} else {
				c.Due = time.Unix(crt+due*86400, 0).UTC()
			}
		default:
			c.Due = time.Unix(crt+due*86400, 0).UTC()
		}
		out[nid] = append(out[nid], c)
	}
	return out, rows.Err()
}

// readSchema11 reads note types and decks from the JSON in the col table.
func readSchema11(db *sql.DB) (map[int64]*notetype, map[int64]string, error) {
	var models, decksJSON string
	if err := db.QueryRow(`SELECT models, decks FROM col LIMIT 1`).Scan(&models, &decksJSON); err != nil {
		return nil, nil, fmt.Errorf("collection: %w", err)
	}
	var ms map[string]struct {
		Type  int `json:"type"`
		Tmpls []struct {
			Ord  int    `json:"ord"`
			Qfmt string `json:"qfmt"`
		} `json:"tmpls"`
	}
	if err := json.Unmarshal([]byte(models), &ms); err != nil {
		return nil, nil, fmt.Errorf("note types: %w", err)
	}
	types := map[int64]*notetype{}
	for k, m := range ms {
		id, err := strconv.ParseInt(k, 10, 64)
		if err != nil {
			continue
		}
		nt := &notetype{cloze: m.Type == 1, templates: len(m.Tmpls)}
		for _, t := range m.Tmpls {
			if t.Ord == 0 && strings.Contains(t.Qfmt, "{{type:") {
				nt.typeIn = true
			}
			if strings.Contains(t.Qfmt, "{{cloze:") {
				nt.cloze = true
			}
		}
		types[id] = nt
	}
	var ds map[string]struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(decksJSON), &ds); err != nil {
		return nil, nil, fmt.Errorf("decks: %w", err)
	}
	decks := map[int64]string{}
	for k, d := range ds {
		if id, err := strconv.ParseInt(k, 10, 64); err == nil {
			decks[id] = deckName(d.Name)
		}
	}
	return types, decks, nil
}

// readSchema18 reads the notetypes, templates and decks tables of newer
// collections. Their config columns are protobuf: NotetypeConfig.kind is
// field 1 (1 = cloze), CardTemplateConfig.q_format is field 1.
func readSchema18(db *sql.DB) (map[int64]*notetype, map[int64]string, error) {
	types := map[int64]*notetype{}
	rows, err := db.Query(`SELECT id, config FROM notetypes`)
	if err != nil {
		return nil, nil, fmt.Errorf("note types: %w", err)
	}
	for rows.Next() {
		var id int64
		var cfg []byte
		if err := rows.Scan(&id, &cfg); err != nil {
			rows.Close()
			return nil, nil, err
		}
		types[id] = &notetype{cloze: protoVarint(cfg, 1) == 1}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	rows, err = db.Query(`SELECT ntid, ord, config FROM templates`)
	if err != nil {
		return nil, nil, fmt.Errorf("templates: %w", err)
	}
	for rows.Next() {
		var ntid int64
		var ord int
		var cfg []byte
		if err := rows.Scan(&ntid, &ord, &cfg); err != nil {
			rows.Close()
			return nil, nil, err
		}
		nt := types[ntid]
		if nt == nil {
			nt = &notetype{}
			types[ntid] = nt
		}
		nt.templates++
		q := protoString(cfg, 1)
		if q == "" {
			q = string(cfg)
		}
		if ord == 0 && strings.Contains(q, "{{type:") {
			nt.typeIn = true
		}
		if bytes.Contains(cfg, []byte("{{cloze:")) {
			nt.cloze = true
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	decks := map[int64]string{}
	rows, err = db.Query(`SELECT id, name FROM decks`)
	if err != nil {
		return nil, nil, fmt.Errorf("decks: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, nil, err
		}
		decks[id] = deckName(name)
	}
	return types, decks, rows.Err()
}

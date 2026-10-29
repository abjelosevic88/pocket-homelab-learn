package server

import (
	"context"
	"crypto/sha1"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-learn/internal/anki"
	"github.com/abjelosevic88/pocket-homelab-learn/internal/store"
)

// ---- Anki ----

// handleImportAnki reads an Anki export (.apkg or .colpkg): decks, notes,
// pictures and sounds, and (unless ?progress=0) where each card stands, so
// nothing has to be learned again. Importing the same file twice adds
// nothing (notes keep their Anki id).
func (s *Server) handleImportAnki(w http.ResponseWriter, r *http.Request) {
	if !s.importMu.TryLock() {
		writeErr(w, http.StatusConflict, "another import is running; try again in a minute")
		return
	}
	defer s.importMu.Unlock()
	ctx := r.Context()
	uid := actorID(ctx)
	keep := r.URL.Query().Get("progress") != "0"
	dir, err := os.MkdirTemp(s.tmpDir(), "anki-*")
	if err != nil {
		s.fail(w, err, "import")
		return
	}
	defer os.RemoveAll(dir)
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxUploadBytes)
	mr, err := r.MultipartReader()
	if err != nil {
		writeErr(w, http.StatusBadRequest, "send the .apkg file")
		return
	}
	file := filepath.Join(dir, "upload.apkg")
	got := false
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeErr(w, http.StatusBadRequest, "the upload broke off: "+err.Error())
			return
		}
		if part.FormName() != "file" {
			continue
		}
		f, err := os.Create(file)
		if err != nil {
			s.fail(w, err, "import")
			return
		}
		_, err = io.Copy(f, part)
		f.Close()
		if err != nil {
			writeErr(w, http.StatusBadRequest, fmt.Sprintf("the file is too large (at most %d MB) or the upload broke off", s.cfg.MaxUploadBytes>>20))
			return
		}
		got = true
		break
	}
	if !got {
		writeErr(w, http.StatusBadRequest, "send the .apkg file")
		return
	}
	pkg, err := anki.Open(file, dir)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	defer pkg.Close()
	counts, err := s.importAnki(context.WithoutCancel(ctx), uid, pkg, keep)
	if err != nil {
		s.fail(w, err, "import")
		return
	}
	writeJSON(w, http.StatusOK, counts)
}

func (s *Server) tmpDir() string {
	d := filepath.Join(s.cfg.DataDir, "tmp")
	_ = os.MkdirAll(d, 0o755)
	return d
}

func (s *Server) importAnki(ctx context.Context, uid int64, pkg *anki.Package, keep bool) (map[string]int, error) {
	counts := map[string]int{"notes": 0, "cards": 0, "decks": 0, "media": 0, "already": 0, "skipped": 0}
	// media first, so renamed files can be rewritten in the notes
	renamed := map[string]string{}
	for _, name := range pkg.Media {
		rc, err := pkg.OpenMedia(name)
		if err != nil {
			continue
		}
		saved, err := s.saveMedia(ctx, uid, name, rc)
		rc.Close()
		if err != nil {
			s.log.Debug("anki import: media skipped", "name", name, "err", err)
			continue
		}
		counts["media"]++
		if saved != name {
			renamed[name] = saved
		}
	}
	rewrite := func(t string) string {
		for from, to := range renamed {
			t = strings.ReplaceAll(t, "media/"+from+")", "media/"+to+")")
			t = strings.ReplaceAll(t, "[sound:"+from+"]", "[sound:"+to+"]")
		}
		return t
	}
	names := map[int64]string{}
	for _, d := range pkg.Decks {
		names[d.ID] = d.Name
	}
	decks := map[int64]*store.Deck{}
	deckOf := func(id int64) (*store.Deck, error) {
		if d, ok := decks[id]; ok {
			return d, nil
		}
		name := names[id]
		if name == "" {
			name = "Imported from Anki"
		}
		_, existed := s.store.GetDeckByName(ctx, uid, name)
		d, err := s.deckFor(ctx, uid, 0, name, "anki")
		if err != nil {
			return nil, err
		}
		if existed != nil {
			counts["decks"]++
		}
		decks[id] = d
		return d, nil
	}
	st, _ := s.store.GetSettings(ctx, uid)
	ck := s.clockFor(st)
	var items []store.ImportItem
	for _, an := range pkg.Notes {
		if len(an.Cards) == 0 {
			continue
		}
		d, err := deckOf(an.Cards[0].DeckID)
		if err != nil {
			return nil, err
		}
		n := store.Note{DeckID: d.ID, Kind: an.Kind, Front: rewrite(an.Front), Back: rewrite(an.Back), Extra: rewrite(an.Extra),
			Tags: strings.Join(an.Tags, " "), Source: "anki", ExternalID: an.GUID}
		if msg := checkNoteBack(&n, true); msg != "" {
			counts["skipped"]++
			continue
		}
		want := map[int]bool{}
		for _, o := range cardOrds(n.Kind, n.Front) {
			want[o] = true
		}
		it := store.ImportItem{Note: n}
		seen := map[int]bool{}
		for _, ac := range an.Cards {
			ord := ac.Ord
			if n.Kind == store.KindCloze {
				ord = ac.Ord + 1 // Anki counts clozes from 0, cards here by their number
			}
			if !want[ord] || seen[ord] {
				continue
			}
			seen[ord] = true
			it.Cards = append(it.Cards, ankiCard(ac, ord, keep, ck))
		}
		for o := range want {
			if !seen[o] {
				it.Cards = append(it.Cards, store.Card{Ord: o})
			}
		}
		if len(it.Cards) == 0 {
			counts["skipped"]++
			continue
		}
		items = append(items, it)
	}
	created, already, err := s.store.ImportBatch(ctx, uid, items)
	if err != nil {
		return nil, err
	}
	counts["notes"], counts["already"] = created, already
	for _, it := range items {
		if it.Note.ID > 0 {
			counts["cards"] += len(it.Cards)
		}
	}
	if created > 0 {
		s.publish(ctx, uid, "learn.cards_added", fmt.Sprintf("%d notes imported from Anki", created), map[string]any{"notes": created, "cards": counts["cards"], "by": "anki"})
	}
	return counts, nil
}

// ankiCard turns an Anki card's progress into an FSRS memory state: the
// interval stands in for the stability (at 90% retention they are about
// the same), the ease for the difficulty.
func ankiCard(ac anki.Card, ord int, keep bool, ck clock) store.Card {
	c := store.Card{Ord: ord, Suspended: ac.Queue == -1, Flag: min(max(ac.Flag, 0), 4)}
	if !keep || ac.Type == 0 {
		return c
	}
	ease := float64(ac.Factor) / 1000
	if ease < 1.3 {
		ease = 2.5
	}
	c.Difficulty = math.Min(10, math.Max(1, 1+(3.0-ease)/(3.0-1.3)*9))
	c.Reps, c.Lapses = ac.Reps, ac.Lapses
	due := ac.Due
	if due.IsZero() {
		due = ck.now
	}
	switch ac.Type {
	case 2:
		ivl := max(ac.Interval, 1)
		c.State, c.Interval, c.Stability = store.StateReview, ivl, float64(ivl)
		c.Due = store.TS(due)
		c.LastReview = store.TS(due.Add(-time.Duration(ivl) * 24 * time.Hour))
	default:
		c.State = store.StateLearning
		if ac.Type == 3 {
			c.State = store.StateRelearning
		}
		c.Stability = math.Max(float64(ac.Interval), 0.5)
		c.Due = store.TS(due)
		c.LastReview = store.TS(ck.now.Add(-time.Hour))
	}
	if c.Reps == 0 {
		c.Reps = 1
	}
	return c
}

// ---- CSV / TSV (Quizlet, spreadsheets) ----

type csvInput struct {
	DeckID    int64  `json:"deck_id"`
	Deck      string `json:"deck"`
	Text      string `json:"text"`
	Delimiter string `json:"delimiter"` // auto | , | ; | tab
	Header    bool   `json:"header"`
	Kind      string `json:"kind"`
	Front     int    `json:"front"` // column numbers from 0; -1 = none
	Back      int    `json:"back"`
	Extra     int    `json:"extra"`
	TagsCol   int    `json:"tags_col"`
	Tags      string `json:"tags"` // added to every note
	DryRun    bool   `json:"dry_run"`
}

func detectDelimiter(text string) rune {
	first := text
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		first = text[:i]
	}
	switch {
	case strings.Contains(first, "\t"):
		return '\t'
	case strings.Count(first, ";") > strings.Count(first, ","):
		return ';'
	case strings.Count(first, ",") > 0:
		return ','
	case strings.Contains(first, " - "):
		return '-'
	}
	return '\t'
}

func parseRows(text string, delim rune) ([][]string, error) {
	if delim == '-' { // "word - meaning" lines
		var rows [][]string
		for _, l := range strings.Split(text, "\n") {
			if a, b, ok := strings.Cut(l, " - "); ok {
				rows = append(rows, []string{strings.TrimSpace(a), strings.TrimSpace(b)})
			}
		}
		return rows, nil
	}
	cr := csv.NewReader(strings.NewReader(text))
	cr.Comma = delim
	cr.LazyQuotes = true
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true
	return cr.ReadAll()
}

// handleImportCSV makes notes from rows of a CSV or TSV text (with dry_run: a preview).
func (s *Server) handleImportCSV(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	in := csvInput{Front: 0, Back: 1, Extra: -1, TagsCol: -1}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	text := strings.TrimPrefix(strings.ReplaceAll(in.Text, "\r\n", "\n"), "\ufeff")
	if strings.TrimSpace(text) == "" {
		writeErr(w, http.StatusBadRequest, "paste or choose some rows")
		return
	}
	delim := detectDelimiter(text)
	switch in.Delimiter {
	case ",":
		delim = ','
	case ";":
		delim = ';'
	case "tab":
		delim = '\t'
	}
	rows, err := parseRows(text, delim)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "the rows could not be read: "+err.Error())
		return
	}
	if in.Header && len(rows) > 0 {
		rows = rows[1:]
	}
	if len(rows) > 50000 {
		writeErr(w, http.StatusBadRequest, "at most 50,000 rows at a time")
		return
	}
	col := func(row []string, i int) string {
		if i < 0 || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}
	kind := in.Kind
	if !store.ValidKind(kind) {
		kind = store.KindBasic
	}
	if in.DryRun {
		cols := 0
		for _, r := range rows {
			cols = max(cols, len(r))
		}
		writeJSON(w, http.StatusOK, map[string]any{"rows": len(rows), "columns": cols, "delimiter": string(delim), "sample": rows[:min(len(rows), 8)]})
		return
	}
	d, err := s.deckFor(ctx, uid, in.DeckID, in.Deck, "csv")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "choose a deck")
		return
	}
	var items []store.ImportItem
	skipped := 0
	for _, row := range rows {
		n := store.Note{DeckID: d.ID, Kind: kind, Front: col(row, in.Front), Back: col(row, in.Back), Extra: col(row, in.Extra),
			Tags: strings.TrimSpace(col(row, in.TagsCol) + " " + in.Tags), Source: "csv"}
		if msg := checkNote(&n); msg != "" {
			skipped++
			continue
		}
		h := sha1.Sum([]byte(fmt.Sprintf("%d\x00%s\x00%s", d.ID, n.Front, n.Back)))
		n.ExternalID = hex.EncodeToString(h[:8])
		it := store.ImportItem{Note: n}
		for _, o := range cardOrds(n.Kind, n.Front) {
			it.Cards = append(it.Cards, store.Card{Ord: o})
		}
		items = append(items, it)
	}
	created, already, err := s.store.ImportBatch(ctx, uid, items)
	if err != nil {
		s.fail(w, err, "import")
		return
	}
	if created > 0 {
		s.publish(ctx, uid, "learn.cards_added", fmt.Sprintf("%d notes added to %s", created, d.Name), map[string]any{"notes": created, "deck": d.Name, "deck_id": d.ID, "by": "csv"})
	}
	writeJSON(w, http.StatusOK, map[string]any{"notes": created, "already": already, "skipped": skipped, "deck_id": d.ID, "deck": d.Name})
}

// ---- Pocket Learn export ----

// exportDoc is the JSON export of one person's data.
type exportDoc struct {
	App      string         `json:"app"`
	Version  string         `json:"version"`
	Exported string         `json:"exported_at"`
	Decks    []store.Deck   `json:"decks"`
	Notes    []store.Note   `json:"notes"`
	Reviews  []store.Review `json:"reviews"`
}

func (s *Server) handleExportJSON(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	doc := exportDoc{App: "pocket-learn", Version: Version, Exported: store.Now()}
	var err error
	if doc.Decks, err = s.store.ListDecks(ctx, uid, true); err == nil {
		if doc.Notes, err = s.store.AllNotes(ctx, uid); err == nil {
			doc.Reviews, err = s.store.AllReviews(ctx, uid)
		}
	}
	if err != nil {
		s.fail(w, err, "export")
		return
	}
	if deck := int64(queryInt(r, "deck")); deck > 0 {
		keep := doc.Decks[:0]
		for _, d := range doc.Decks {
			if d.ID == deck {
				keep = append(keep, d)
			}
		}
		doc.Decks = keep
		cards := map[int64]bool{}
		notes := doc.Notes[:0]
		for _, n := range doc.Notes {
			if n.DeckID == deck {
				notes = append(notes, n)
				for _, c := range n.Cards {
					cards[c.ID] = true
				}
			}
		}
		doc.Notes = notes
		revs := doc.Reviews[:0]
		for _, rv := range doc.Reviews {
			if cards[rv.CardID] {
				revs = append(revs, rv)
			}
		}
		doc.Reviews = revs
	}
	w.Header().Set("Content-Disposition", `attachment; filename="pocket-learn-export.json"`)
	writeJSON(w, http.StatusOK, doc)
}

// handleExportCSV writes every note as a row (deck, kind, front, back, extra, tags, due).
func (s *Server) handleExportCSV(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	notes, err := s.store.AllNotes(ctx, uid)
	if err != nil {
		s.fail(w, err, "export")
		return
	}
	decks, _ := s.store.ListDecks(ctx, uid, true)
	names := map[int64]string{}
	for _, d := range decks {
		names[d.ID] = d.Name
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="pocket-learn-cards.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"deck", "kind", "front", "back", "extra", "tags", "source", "created", "next_due", "interval_days", "lapses"})
	for _, n := range notes {
		due, ivl, lapses := "", 0, 0
		for _, c := range n.Cards {
			if c.State != store.StateNew && (due == "" || c.Due < due) {
				due = c.Due
			}
			ivl = max(ivl, c.Interval)
			lapses += c.Lapses
		}
		_ = cw.Write([]string{names[n.DeckID], n.Kind, n.Front, n.Back, n.Extra, n.Tags, n.Source, n.CreatedAt, due, strconv.Itoa(ivl), strconv.Itoa(lapses)})
	}
	cw.Flush()
}

// handleImportJSON merges an export into the caller's own data: decks by
// name, notes with their cards' memory state and their review history.
// Importing the same file twice adds nothing.
func (s *Server) handleImportJSON(w http.ResponseWriter, r *http.Request) {
	var doc exportDoc
	if err := decode(r, &doc); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if doc.App != "" && doc.App != "pocket-learn" {
		writeErr(w, http.StatusBadRequest, "this is not a Pocket Learn export")
		return
	}
	ctx := r.Context()
	uid := actorID(ctx)
	counts := map[string]int{"decks": 0, "notes": 0, "already": 0, "skipped": 0}
	decks := map[int64]*store.Deck{}
	for _, d := range doc.Decks {
		if _, err := s.store.GetDeckByName(ctx, uid, d.Name); errors.Is(err, store.ErrNotFound) {
			nd := d
			nd.ID, nd.Sort, nd.Source, nd.Counts = 0, 0, "import", nil
			if strings.TrimSpace(nd.Name) == "" {
				continue
			}
			if err := s.store.CreateDeck(ctx, uid, &nd); err == nil {
				counts["decks"]++
				decks[d.ID] = &nd
				continue
			}
		}
		if ex, err := s.store.GetDeckByName(ctx, uid, d.Name); err == nil {
			decks[d.ID] = ex
		}
	}
	revByCard := map[int64][]store.Review{}
	for _, rv := range doc.Reviews {
		revByCard[rv.CardID] = append(revByCard[rv.CardID], rv)
	}
	var items []store.ImportItem
	for _, n := range doc.Notes {
		d := decks[n.DeckID]
		if d == nil {
			counts["skipped"]++
			continue
		}
		ext := fmt.Sprintf("learn:%d", n.ID)
		if n.ExternalID != "" {
			ext = n.Source + ":" + n.ExternalID
		}
		nn := store.Note{DeckID: d.ID, Kind: n.Kind, Front: n.Front, Back: n.Back, Extra: n.Extra, Tags: n.Tags, Source: "import",
			SourceRef: n.SourceRef, SourceURL: n.SourceURL, ExternalID: ext, CreatedAt: n.CreatedAt}
		if msg := checkNoteBack(&nn, true); msg != "" {
			counts["skipped"]++
			continue
		}
		it := store.ImportItem{Note: nn, Reviews: map[int][]store.Review{}}
		want := map[int]bool{}
		for _, o := range cardOrds(nn.Kind, nn.Front) {
			want[o] = true
		}
		for _, c := range n.Cards {
			if !want[c.Ord] {
				continue
			}
			delete(want, c.Ord)
			old := c.ID
			c.ID, c.Position = 0, 0
			if c.State < 0 || c.State > 3 {
				c.State = store.StateNew
			}
			it.Cards = append(it.Cards, c)
			it.Reviews[c.Ord] = revByCard[old]
		}
		for o := range want {
			it.Cards = append(it.Cards, store.Card{Ord: o})
		}
		items = append(items, it)
	}
	created, already, err := s.store.ImportBatch(ctx, uid, items)
	if err != nil {
		s.fail(w, err, "import")
		return
	}
	counts["notes"], counts["already"] = created, already
	writeJSON(w, http.StatusOK, counts)
}

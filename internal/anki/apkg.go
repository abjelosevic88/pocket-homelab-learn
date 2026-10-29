// Package anki reads Anki exports (.apkg decks and .colpkg collections):
// decks, notes turned into plain text, card scheduling and media.
package anki

import (
	"archive/zip"
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

// Deck is an Anki deck; "A::B" hierarchies become "A › B".
type Deck struct {
	ID   int64
	Name string
}

// Card is one Anki card of a note and its scheduling.
type Card struct {
	Ord      int
	DeckID   int64
	Type     int       // 0 new, 1 learning, 2 review, 3 relearning
	Queue    int       // -1 suspended, -2/-3 buried, else as Anki
	Due      time.Time // zero when new
	Interval int       // days
	Factor   int       // ease ×1000 (2500)
	Reps     int
	Lapses   int
	Flag     int // 0–7
}

// Note is an Anki note with its fields converted by HTMLToText.
type Note struct {
	GUID  string
	Kind  string // basic | reverse | cloze | type
	Front string
	Back  string
	Extra string
	Tags  []string
	Cards []Card
}

// Package is an opened export.
type Package struct {
	Decks             []Deck
	Notes             []Note
	Media             []string // media file names the notes use and the package has
	CollectionCreated time.Time

	zr      *zip.ReadCloser
	media   map[string]string // file name → zip entry
	tmpFile string
}

// ErrNotAnki means the file is not an Anki export.
var ErrNotAnki = errors.New("this is not an Anki export (.apkg or .colpkg)")

// Open reads an export: the collection is extracted (and decompressed) into
// tmpDir, read with SQLite and removed again by Close. The zip stays open
// for OpenMedia until Close.
func Open(apkgPath, tmpDir string) (*Package, error) {
	zr, err := zip.OpenReader(apkgPath)
	if err != nil {
		return nil, ErrNotAnki
	}
	p := &Package{zr: zr, media: map[string]string{}}
	ok := false
	defer func() {
		if !ok {
			_ = p.Close()
		}
	}()
	entries := map[string]*zip.File{}
	for _, f := range zr.File {
		entries[f.Name] = f
	}
	var col *zip.File
	compressed := false
	switch {
	case entries["collection.anki21b"] != nil:
		col, compressed = entries["collection.anki21b"], true
	case entries["collection.anki21"] != nil:
		col = entries["collection.anki21"]
	case entries["collection.anki2"] != nil:
		col = entries["collection.anki2"]
	default:
		return nil, ErrNotAnki
	}
	tmp, err := os.CreateTemp(tmpDir, "anki-*.sqlite")
	if err != nil {
		return nil, err
	}
	p.tmpFile = tmp.Name()
	if err := extract(col, tmp, compressed); err != nil {
		tmp.Close()
		return nil, fmt.Errorf("reading the collection: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}

	byEntry := map[string]string{}
	if m := entries["media"]; m != nil {
		rc, err := m.Open()
		if err != nil {
			return nil, err
		}
		raw, err := io.ReadAll(io.LimitReader(rc, 64<<20))
		rc.Close()
		if err != nil {
			return nil, err
		}
		if byEntry, err = parseMediaMap(raw); err != nil {
			return nil, err
		}
	}
	for entry, name := range byEntry {
		if entries[entry] != nil && name != "" {
			p.media[name] = entry
		}
	}
	if err := p.read(); err != nil {
		return nil, err
	}
	ok = true
	return p, nil
}

func extract(f *zip.File, dst io.Writer, compressed bool) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	var src io.Reader = rc
	if compressed {
		d, err := newZstdReader(rc)
		if err != nil {
			return err
		}
		defer d.Close()
		src = d
	}
	n, err := io.Copy(dst, io.LimitReader(src, maxCollection+1))
	if err != nil {
		return err
	}
	if n > maxCollection {
		return errors.New("the collection is larger than 2 GB")
	}
	return nil
}

// OpenMedia returns a media file's bytes (decompressed when the package
// stores media zstd-compressed). The caller limits how much it reads.
func (p *Package) OpenMedia(name string) (io.ReadCloser, error) {
	entry, ok := p.media[name]
	if !ok || p.zr == nil {
		return nil, os.ErrNotExist
	}
	f, err := p.zr.Open(entry)
	if err != nil {
		return nil, err
	}
	br := bufio.NewReader(f)
	head, _ := br.Peek(4)
	if isZstd(head) {
		d, err := newZstdReader(br)
		if err != nil {
			f.Close()
			return nil, err
		}
		return &zstdReadCloser{d: d, src: f}, nil
	}
	return struct {
		io.Reader
		io.Closer
	}{br, f}, nil
}

// Close releases the zip and removes the extracted collection.
func (p *Package) Close() error {
	var err error
	if p.zr != nil {
		err = p.zr.Close()
		p.zr = nil
	}
	if p.tmpFile != "" {
		for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
			_ = os.Remove(p.tmpFile + suffix)
		}
		p.tmpFile = ""
	}
	return err
}

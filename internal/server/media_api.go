package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/abjelosevic88/pocket-homelab-learn/internal/store"
)

// ---- pictures and sounds ----
//
// A card refers to a file by name: ![](media/cat.jpg) or [sound:hello.mp3].
// Files are stored once per person by content (FILES_DIR/<user>/<sha256>),
// names are per person; the same name with other content gets a new name.

const maxMediaBytes = 50 << 20

var mediaTypes = map[string]string{
	".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".gif": "image/gif", ".webp": "image/webp", ".svg": "image/svg+xml", ".avif": "image/avif", ".bmp": "image/bmp",
	".mp3": "audio/mpeg", ".m4a": "audio/mp4", ".ogg": "audio/ogg", ".oga": "audio/ogg", ".opus": "audio/ogg", ".wav": "audio/wav", ".flac": "audio/flac", ".aac": "audio/aac",
	".mp4": "video/mp4", ".webm": "video/webm",
}

var unsafeName = regexp.MustCompile(`[^\p{L}\p{N}._ ()-]+`)

// cleanMediaName keeps a safe base name with a known extension ("" = not allowed).
func cleanMediaName(name string) string {
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	ext := strings.ToLower(filepath.Ext(name))
	if _, ok := mediaTypes[ext]; !ok {
		return ""
	}
	base := strings.TrimSpace(unsafeName.ReplaceAllString(strings.TrimSuffix(name, filepath.Ext(name)), "_"))
	base = strings.Trim(base, ". ")
	if base == "" {
		base = "file"
	}
	if r := []rune(base); len(r) > 80 {
		base = string(r[:80])
	}
	return base + ext
}

func (s *Server) mediaPath(uid int64, hash string) string {
	return filepath.Join(s.cfg.FilesDir, fmt.Sprint(uid), hash)
}

// saveMedia stores a file for a person and returns the name cards use. The
// same content under the same name is kept once; another file with a taken
// name becomes "name-2.ext".
func (s *Server) saveMedia(ctx context.Context, uid int64, name string, r io.Reader) (string, error) {
	clean := cleanMediaName(name)
	if clean == "" {
		return "", fmt.Errorf("%s: only pictures, sounds and short videos", name)
	}
	dir := filepath.Join(s.cfg.FilesDir, fmt.Sprint(uid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, ".up-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(r, maxMediaBytes+1))
	tmp.Close()
	if err != nil {
		return "", err
	}
	if n > maxMediaBytes {
		return "", fmt.Errorf("%s is larger than 50 MB", name)
	}
	hash := hex.EncodeToString(h.Sum(nil))[:40]
	ext := strings.ToLower(filepath.Ext(clean))
	stem := strings.TrimSuffix(clean, filepath.Ext(clean))
	for i := 1; i < 1000; i++ {
		cand := clean
		if i > 1 {
			cand = fmt.Sprintf("%s-%d%s", stem, i, ext)
		}
		m, err := s.store.GetMedia(ctx, uid, cand)
		if err == nil {
			if m.Hash == hash {
				return cand, nil
			}
			continue
		}
		dst := s.mediaPath(uid, hash)
		if _, err := os.Stat(dst); err != nil {
			if err := os.Rename(tmp.Name(), dst); err != nil {
				return "", err
			}
		}
		if err := s.store.AddMedia(ctx, uid, &store.Media{Name: cand, Hash: hash, Mime: mediaTypes[ext], Size: n}); err != nil {
			if err == store.ErrConflict {
				continue
			}
			return "", err
		}
		return cand, nil
	}
	return "", fmt.Errorf("too many files called %s", clean)
}

// handleUploadMedia takes a picture or a sound pasted or dropped into a card.
func (s *Server) handleUploadMedia(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	r.Body = http.MaxBytesReader(w, r.Body, maxMediaBytes+1<<20)
	f, hdr, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "send a file (up to 50 MB)")
		return
	}
	defer f.Close()
	name := hdr.Filename
	if filepath.Ext(name) == "" {
		if exts, _ := mime.ExtensionsByType(hdr.Header.Get("Content-Type")); len(exts) > 0 {
			name += exts[len(exts)-1]
		}
	}
	saved, err := s.saveMedia(ctx, actorID(ctx), name, f)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": saved, "url": "/api/v1/media/" + saved})
}

// handleMedia serves one of the person's files to the cards.
func (s *Server) handleMedia(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	m, err := s.store.GetMedia(ctx, uid, chiParam(r, "name"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	f, err := os.Open(s.mediaPath(uid, m.Hash))
	if err != nil {
		writeErr(w, http.StatusNotFound, "the file is missing")
		return
	}
	defer f.Close()
	fi, _ := f.Stat()
	w.Header().Set("Content-Type", m.Mime)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if m.Mime == "image/svg+xml" {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	}
	http.ServeContent(w, r, m.Name, fi.ModTime(), f)
}

// cleanTemp removes import leftovers older than a day.
func (s *Server) cleanTemp() {
	dir := filepath.Join(s.cfg.DataDir, "tmp")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if fi, err := e.Info(); err == nil && s.now().Sub(fi.ModTime()).Hours() > 24 {
			_ = os.RemoveAll(filepath.Join(dir, e.Name()))
		}
	}
}

// Package pocket talks to sibling Pocket apps: Pocket Reader and Pocket
// Library (highlights to turn into cards) and Pocket Notes (a note to make
// cards from). Same bearer-token JSON API everywhere.
package pocket

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is a bearer-token JSON client for one Pocket app.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// New creates a client.
func New(baseURL, token string, timeout time.Duration) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, HTTP: &http.Client{Timeout: timeout}}
}

// Configured reports whether URL and token are set.
func (c *Client) Configured() bool { return c != nil && c.BaseURL != "" && c.Token != "" }

// ErrNotFound is a 404 from the sibling app.
var ErrNotFound = errors.New("not found")

func (c *Client) do(ctx context.Context, method, path string, q url.Values, body any, v any) error {
	if !c.Configured() {
		return errors.New("not configured")
	}
	u := c.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized {
		return errors.New("token rejected (401)")
	}
	if res.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if res.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		b, _ := io.ReadAll(io.LimitReader(res.Body, 2000))
		_ = json.Unmarshal(b, &e)
		if e.Error != "" {
			return fmt.Errorf("HTTP %d: %s", res.StatusCode, e.Error)
		}
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	if v == nil || res.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(v)
}

// Health is the connection test for any Pocket app: /healthz plus a token check.
func (c *Client) Health(ctx context.Context) (string, error) {
	var out struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	if !c.Configured() {
		return "", errors.New("not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/healthz", nil)
	if err != nil {
		return "", err
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", res.StatusCode)
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	var me map[string]any
	if err := c.do(ctx, http.MethodGet, "/api/v1/auth/me", nil, nil, &me); err != nil {
		return out.Version, err
	}
	return out.Version, nil
}

// ---------------- Pocket Reader ----------------

// Highlight is a passage highlighted in Pocket Reader.
type Highlight struct {
	ID           int64  `json:"id"`
	ArticleID    int64  `json:"article_id"`
	Text         string `json:"text"`
	Note         string `json:"note"`
	ArticleTitle string `json:"article_title"`
	ArticleURL   string `json:"article_url"`
	SiteName     string `json:"site_name"`
	CreatedAt    string `json:"created_at"`
}

// Highlights lists the person's highlights in Pocket Reader, newest first.
func (c *Client) Highlights(ctx context.Context, query string, limit int) ([]Highlight, error) {
	q := url.Values{"limit": {fmt.Sprint(limit)}}
	if query != "" {
		q.Set("q", query)
	}
	var out []Highlight
	err := c.do(ctx, http.MethodGet, "/api/v1/highlights", q, nil, &out)
	return out, err
}

// ---------------- Pocket Library ----------------

// Annotation is a highlight or a note made in a book in Pocket Library.
type Annotation struct {
	ID        int64  `json:"id"`
	BookID    int64  `json:"book_id"`
	Kind      string `json:"kind"` // highlight | note | bookmark
	Text      string `json:"text"`
	Note      string `json:"note"`
	Chapter   string `json:"chapter"`
	BookTitle string `json:"book_title"`
	CreatedAt string `json:"created_at"`
}

// Annotations lists the person's book highlights and notes in Pocket Library.
func (c *Client) Annotations(ctx context.Context, query string, limit int) ([]Annotation, error) {
	q := url.Values{"limit": {fmt.Sprint(limit)}}
	if query != "" {
		q.Set("q", query)
	}
	var out []Annotation
	err := c.do(ctx, http.MethodGet, "/api/v1/annotations", q, nil, &out)
	return out, err
}

// ---------------- Pocket Notes ----------------

// Note is a note in Pocket Notes.
type Note struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	Category string `json:"category"`
	Content  string `json:"content"`
	Snippet  string `json:"snippet"`
	Modified int64  `json:"modified"`
}

// SearchNotes finds the person's notes (the most recent ones for an empty query).
func (c *Client) SearchNotes(ctx context.Context, query string, limit int) ([]Note, error) {
	q := url.Values{"limit": {fmt.Sprint(limit)}}
	if query != "" {
		q.Set("q", query)
	}
	var out []Note
	err := c.do(ctx, http.MethodGet, "/api/v1/search", q, nil, &out)
	return out, err
}

// GetNote reads one note with its text.
func (c *Client) GetNote(ctx context.Context, id int64) (*Note, error) {
	var out Note
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/notes/%d", id), url.Values{"peek": {"1"}}, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

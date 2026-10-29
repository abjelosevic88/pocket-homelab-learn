package server

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-learn/internal/store"
)

// Pocket Core app links (docs/links.md in pocket-homelab-core), Learn as the
// target: another Pocket app the person allowed in Core may act here for
// them with a pcx_ token, only within these scopes. Every route not listed
// in linkRoutes stays closed to it (403).

var linkOffers = []core.Offer{
	{Scope: "learn:read", Name: "See what you study", Description: "Read your decks, how many cards are due, what you studied each day and your streak — for your journal, dashboards and reminders. Never the cards themselves."},
	{Scope: "learn:write", Name: "Add flashcards", Description: "Add flashcards to your decks (a passage you highlighted, a word from a note). It may change or remove only the cards it added itself."},
}

type linkRoute struct {
	methods string
	path    *regexp.Regexp
	scopes  string // any of these (space separated); "" = any linked app
}

var linkRoutes = []linkRoute{
	{"GET", regexp.MustCompile(`^/api/v1/auth/me$`), ""},
	{"GET", regexp.MustCompile(`^/api/v1/(dashboard|stats)$`), "learn:read"},
	{"GET", regexp.MustCompile(`^/api/v1/(day|summary)/\d{4}-\d{2}-\d{2}$`), "learn:read"},
	{"GET", regexp.MustCompile(`^/api/v1/decks$`), "learn:read learn:write"},
	{"GET", regexp.MustCompile(`^/api/v1/decks/\d+$`), "learn:read"},
	{"POST", regexp.MustCompile(`^/api/v1/notes$`), "learn:write"},
	{"GET PUT DELETE", regexp.MustCompile(`^/api/v1/notes/\d+$`), "learn:write"},
}

var errLinkScope = errors.New("outside the link's scope")

// linkAllows reports whether a link token with these scopes may make this request.
func linkAllows(scopes []string, r *http.Request) bool {
	for _, lr := range linkRoutes {
		if !strings.Contains(" "+lr.methods+" ", " "+r.Method+" ") || !lr.path.MatchString(r.URL.Path) {
			continue
		}
		if lr.scopes == "" {
			return true
		}
		for _, sc := range scopes {
			for _, want := range strings.Fields(lr.scopes) {
				if sc == want {
					return true
				}
			}
		}
	}
	return false
}

// linkUser validates a pcx_ token, checks the route against its scopes and
// maps it to the person it acts for. The second value is the acting app's slug.
func (s *Server) linkUser(ctx context.Context, tok string, r *http.Request) (*store.User, string, error) {
	in, err := s.core.Introspect(ctx, tok)
	if err != nil {
		return nil, "", err
	}
	if !in.Active || !in.Delegated() || in.Audience != "learn" || in.User == nil || in.User.Email == "" || in.App == nil || in.App.Slug == "" {
		return nil, "", http.ErrNoCookie
	}
	if !linkAllows(strings.Fields(in.Scope), r) {
		return nil, "", errLinkScope
	}
	u, err := s.coreIdentityUser(ctx, in)
	return u, in.App.Slug, err
}

const linkAppKey ctxKey = 2

// linkApp is the acting app's slug when the request came in on a link token.
func linkApp(ctx context.Context) string {
	a, _ := ctx.Value(linkAppKey).(string)
	return a
}

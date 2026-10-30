package server

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-learn/internal/srs"
	"github.com/abjelosevic88/pocket-homelab-learn/internal/store"
)

// seedDemo fills the owner's account with five decks and four months of
// plausible studying, run through the real scheduler (DEMO_DATA=true, for
// screenshots and trying the app). Runs once.
func (s *Server) seedDemo(ctx context.Context, owner int64) error {
	key := fmt.Sprintf("seeded:%d:demo", owner)
	if _, _, done := s.store.CacheGet(ctx, key); done {
		return nil
	}
	rng := rand.New(rand.NewSource(42))
	st, _ := s.store.GetSettings(ctx, owner)
	ck := s.clockFor(st)
	now := ck.now
	type deckSpec struct {
		name, desc, color, kind string
		from, perDay            int // first day (days ago) and new cards a day
		notes                   [][3]string
	}
	specs := []deckSpec{
		{"Spanish — everyday words", "The 300 words you hear every day, both ways.", "#e8590c", store.KindReverse, 118, 6, spanish},
		{"World capitals", "Every country's capital.", "#1c7ed6", store.KindBasic, 104, 4, capitals},
		{"The human body", "Anatomy and physiology, from a first-year course.", "#d6336c", store.KindCloze, 86, 3, body},
		{"Go programming", "The language, the standard library and the tools.", "#0ca678", store.KindBasic, 58, 2, golang},
		{"Highlights", "Passages from articles and books worth keeping.", "#7048e8", store.KindBasic, 40, 1, highlights},
	}
	p := s.params(st, nil)
	type simCard struct {
		c       store.Card
		intro   int // days ago the card is first studied
		forgets float64
	}
	var sims []*simCard
	for _, sp := range specs {
		d := &store.Deck{Name: sp.name, Description: sp.desc, Color: sp.color, NewPerDay: sp.perDay + 4, ReviewsPerDay: 200, NewOrder: "added"}
		if err := s.store.CreateDeck(ctx, owner, d); err != nil {
			return err
		}
		for i, n := range sp.notes {
			kind := sp.kind
			if sp.name == "Go programming" && i%4 == 3 {
				kind = store.KindType
			}
			note := &store.Note{DeckID: d.ID, Kind: kind, Front: n[0], Back: n[1], Tags: n[2]}
			if kind == store.KindCloze {
				note.Back, note.Extra = "", n[1]
			}
			if sp.name == "Highlights" {
				note.Source, note.SourceRef = "highlight", strings.TrimPrefix(n[1], "— ")
			}
			// spread over the deck's months; the last few are still new today
			intro := sp.from - int(float64(i)/float64(len(sp.notes))*float64(sp.from+6))
			if intro < 0 {
				intro = -1
			}
			note.CreatedAt = store.TS(now.AddDate(0, 0, -max(intro, 0)-1))
			if err := s.store.CreateNote(ctx, owner, note, cardOrds(note.Kind, note.Front)); err != nil {
				return fmt.Errorf("demo note %q: %w", n[0], err)
			}
			for _, c := range note.Cards {
				sims = append(sims, &simCard{c: c, intro: intro, forgets: 0.6 + rng.Float64()*0.8})
			}
		}
	}
	var reviews []store.Review
	answer := func(sc *simCard, at time.Time) time.Duration {
		cs := toSRS(&sc.c)
		el := ck.elapsedDays(cs.LastReview)
		if !cs.LastReview.IsZero() {
			a, _ := store.ParseDate(ck.day(cs.LastReview))
			b, _ := store.ParseDate(ck.day(at))
			el = int(b.Sub(a).Hours()/24 + 0.5)
		}
		rating := srs.Good
		ret := 0.0
		if sc.c.State == store.StateNew {
			switch x := rng.Float64(); {
			case x < 0.22*sc.forgets:
				rating = srs.Again
			case x > 0.9:
				rating = srs.Easy
			}
		} else {
			ret = p.Retrievability(cs, float64(el))
			r := 1 - (1-ret)*sc.forgets
			if sc.c.State != store.StateReview {
				r = 0.85
			}
			switch x := rng.Float64(); {
			case x > r:
				rating = srs.Again
			case x > r-0.1:
				rating = srs.Hard
			case x < 0.07:
				rating = srs.Easy
			}
		}
		o := p.Review(cs, rating, at, el, seedOf(&sc.c))
		reviews = append(reviews, store.Review{CardID: sc.c.ID, DeckID: sc.c.DeckID, Rating: int(rating), State: sc.c.State, ElapsedDays: el, LastInterval: sc.c.Interval,
			Interval: o.IntervalDays, Stability: o.Card.Stability, Difficulty: o.Card.Difficulty, Retrievability: round2(ret),
			DurationMS: 2500 + rng.Intn(9000), ReviewedAt: store.TS(at), Day: ck.day(at)})
		c := &sc.c
		c.State, c.Step, c.Stability, c.Difficulty = int(o.Card.State), o.Card.Step, o.Card.Stability, o.Card.Difficulty
		c.Due, c.LastReview, c.Reps, c.Lapses, c.Interval = store.TS(o.Card.Due), store.TS(at), o.Card.Reps, o.Card.Lapses, o.IntervalDays
		return o.Wait
	}
	today := ck.today()
	for ago := 120; ago >= 0; ago-- {
		day := store.AddDays(today, -ago)
		if ago > 21 && rng.Float64() < 0.16 {
			continue // a day off now and then; the last three weeks are a streak
		}
		start := ck.dayStart(day).Add(time.Duration(14+rng.Intn(4))*time.Hour + time.Duration(rng.Intn(60))*time.Minute) // evenings
		if ago == 0 {
			start = ck.dayStart(day).Add(time.Duration(3+rng.Intn(2)) * time.Hour) // this morning
		}
		if start.After(now) {
			start = now.Add(-2 * time.Hour)
		}
		at := start
		end := ck.dayStart(store.AddDays(day, 1))
		var queue []*simCard
		for _, sc := range sims {
			switch {
			case sc.c.State == store.StateNew && sc.intro >= ago && sc.intro >= 0:
				queue = append(queue, sc)
			case sc.c.State != store.StateNew && sc.c.Due < store.TS(end):
				queue = append(queue, sc)
			}
		}
		sort.SliceStable(queue, func(i, j int) bool { return queue[i].c.Due < queue[j].c.Due })
		if ago == 0 {
			queue = queue[:len(queue)*2/5] // half done this morning, the rest still due
		}
		for _, sc := range queue {
			for steps := 0; steps < 6; steps++ {
				wait := answer(sc, at)
				at = at.Add(time.Duration(4+rng.Intn(10)) * time.Second)
				if wait <= 0 || wait > 30*time.Minute {
					break
				}
				at = at.Add(wait)
			}
			if at.After(now) {
				break
			}
		}
	}
	cards := make([]store.Card, 0, len(sims))
	for _, sc := range sims {
		cards = append(cards, sc.c)
	}
	sort.Slice(reviews, func(i, j int) bool { return reviews[i].ReviewedAt < reviews[j].ReviewedAt })
	if err := s.store.RestoreState(ctx, owner, cards, reviews); err != nil {
		return err
	}
	s.seedDemoSessions(ctx, owner, reviews)
	if st.NotifyURL == "" {
		st.ReminderTime = "19:00"
		_ = s.store.SaveUserSettings(ctx, owner, st)
	}
	s.log.Info("demo data seeded", "cards", len(cards), "reviews", len(reviews))
	return s.store.CachePut(ctx, key, "1")
}

// seedDemoSessions makes the study sessions the demo answers came from:
// answers with no gap over 15 minutes are one session (of one deck, or of
// all decks when they were mixed); every fourth was timed in Pocket Time.
func (s *Server) seedDemoSessions(ctx context.Context, owner int64, reviews []store.Review) {
	decks, _ := s.store.ListDecks(ctx, owner, true)
	names := map[int64]string{}
	for _, d := range decks {
		names[d.ID] = d.Name
	}
	var cur *store.StudySession
	var last time.Time
	var spent time.Duration // the demo's answers are spread out: a session lasts what its cards took
	n := 0
	flush := func() {
		if cur == nil {
			return
		}
		start, _ := store.ParseStamp(cur.StartedAt)
		end := start.Add(spent + time.Duration(cur.Cards)*4*time.Second)
		cur.EndedAt, cur.LastAt, cur.DurationS = store.Stamp(end), store.Stamp(end), int(end.Sub(start).Seconds())
		spent = 0
		if n%4 == 3 {
			cur.Source, cur.ExternalID = "time", fmt.Sprintf("time:%d", 9000+n)
		}
		_ = s.store.CreateStudySession(ctx, owner, cur)
		n++
		cur = nil
	}
	for _, r := range reviews {
		t := store.ParseTS(r.ReviewedAt)
		if cur != nil && t.Sub(last) > sessionIdle {
			flush()
		}
		if cur == nil {
			id := r.DeckID
			start := t.Add(-time.Duration(r.DurationMS) * time.Millisecond)
			cur = &store.StudySession{DeckID: &id, DeckName: names[id], StartedAt: store.Stamp(start), Date: store.LocalDate(start), Source: "app"}
		} else if cur.DeckID != nil && *cur.DeckID != r.DeckID {
			cur.DeckID, cur.DeckName = nil, ""
		}
		cur.Cards++
		if r.Rating == 1 {
			cur.Again++
		}
		spent += time.Duration(r.DurationMS) * time.Millisecond
		last = t
	}
	flush()
}

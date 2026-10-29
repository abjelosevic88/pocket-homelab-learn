package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/abjelosevic88/pocket-homelab-learn/internal/srs"
	"github.com/abjelosevic88/pocket-homelab-learn/internal/store"
)

// ---- the study day ----
//
// A study day starts at the person's day start hour (4 in the morning by
// default), so a late evening session still counts for that evening.
// Review cards are due on a day: everything due before the end of today's
// study day is shown today. Learning steps are minutes: a card in a step is
// shown when its time has come (or up to 20 minutes early when nothing else
// is left).

const learnAhead = 20 * time.Minute

type clock struct {
	now   time.Time
	start int // day start hour
}

func (s *Server) clockFor(st store.Settings) clock {
	return clock{now: s.now(), start: st.DayStartHour}
}

// day is the study day of a moment.
func (c clock) day(t time.Time) string {
	return t.In(time.Local).Add(-time.Duration(c.start) * time.Hour).Format("2006-01-02")
}

func (c clock) today() string { return c.day(c.now) }

// dayStart is when a study day begins.
func (c clock) dayStart(day string) time.Time {
	d, err := time.ParseInLocation("2006-01-02", day, time.Local)
	if err != nil {
		return c.now
	}
	return d.Add(time.Duration(c.start) * time.Hour)
}

func (c clock) dayEnd() time.Time { return c.dayStart(store.AddDays(c.today(), 1)) }

func (c clock) window() store.CountWindow {
	return store.CountWindow{Now: store.TS(c.now), LearnAhead: store.TS(c.now.Add(learnAhead)), DayEnd: store.TS(c.dayEnd()),
		TomorrowEnd: store.TS(c.dayStart(store.AddDays(c.today(), 2))), Today: c.today()}
}

// elapsedDays is how many study days lie between a moment and today.
func (c clock) elapsedDays(t time.Time) int {
	if t.IsZero() {
		return 0
	}
	a, _ := store.ParseDate(c.day(t))
	b, _ := store.ParseDate(c.today())
	return int(b.Sub(a).Hours()/24 + 0.5)
}

// ---- scheduler ----

// params is the scheduler for a person (and a deck's own retention).
func (s *Server) params(st store.Settings, d *store.Deck) srs.Params {
	p := srs.DefaultParams()
	if st.Retention >= 0.7 && st.Retention <= 0.99 {
		p.DesiredRetention = st.Retention
	}
	if d != nil && d.Retention >= 0.7 && d.Retention <= 0.99 {
		p.DesiredRetention = d.Retention
	}
	if steps, err := srs.ParseSteps(st.LearningSteps); err == nil {
		p.LearningSteps = steps
	}
	if steps, err := srs.ParseSteps(st.RelearningSteps); err == nil {
		p.RelearningSteps = steps
	}
	if st.MaxInterval >= 1 && st.MaxInterval <= 36500 {
		p.MaximumInterval = st.MaxInterval
	}
	return p
}

func toSRS(c *store.Card) srs.Card {
	return srs.Card{State: srs.State(c.State), Step: c.Step, Stability: c.Stability, Difficulty: c.Difficulty,
		Due: store.ParseTS(c.Due), LastReview: store.ParseTS(c.LastReview), Reps: c.Reps, Lapses: c.Lapses}
}

func seedOf(c *store.Card) int64 { return c.ID*7919 + int64(c.Reps) }

// outcome is what one answer would do.
type outcome struct {
	Rating int    `json:"rating"`
	Days   int    `json:"days"`   // > 0: due again in this many days
	WaitS  int    `json:"wait_s"` // > 0: a learning step of this many seconds
	Label  string `json:"label"`  // "10m", "4d", "2.1mo"
}

func intervalLabel(days int, wait time.Duration) string {
	if days <= 0 {
		switch {
		case wait < time.Hour:
			return fmt.Sprintf("%dm", max(1, int(wait.Minutes()+0.5)))
		case wait < 24*time.Hour:
			return fmt.Sprintf("%.0fh", wait.Hours())
		}
		return fmt.Sprintf("%.0fd", wait.Hours()/24)
	}
	switch {
	case days < 30:
		return fmt.Sprintf("%dd", days)
	case days < 365:
		return trimZero(fmt.Sprintf("%.1fmo", float64(days)/30.44))
	}
	return trimZero(fmt.Sprintf("%.1fy", float64(days)/365.25))
}

func trimZero(s string) string {
	for _, suf := range []string{"mo", "y"} {
		if len(s) > len(suf)+2 && s[len(s)-len(suf)-2:len(s)-len(suf)] == ".0" {
			return s[:len(s)-len(suf)-2] + suf
		}
	}
	return s
}

// preview is the four answers' outcomes for a card.
func (s *Server) preview(p srs.Params, ck clock, c *store.Card) []outcome {
	out := make([]outcome, 0, 4)
	sc := toSRS(c)
	el := ck.elapsedDays(sc.LastReview)
	for r := srs.Again; r <= srs.Easy; r++ {
		o := p.Review(sc, r, ck.now, el, seedOf(c))
		out = append(out, outcome{Rating: int(r), Days: o.IntervalDays, WaitS: int(o.Wait.Seconds()), Label: intervalLabel(o.IntervalDays, o.Wait)})
	}
	return out
}

// ---- the queue ----

// studyCard is a card on the study screen.
type studyCard struct {
	*store.CardBrief
	Answers        []outcome `json:"answers"`
	Retrievability float64   `json:"retrievability"` // the chance of remembering it now (0 for new cards)
}

// queueCounts are what is left today in the scope.
type queueCounts struct {
	New    int `json:"new"`
	Learn  int `json:"learn"`
	Review int `json:"review"`
	Done   int `json:"done"` // answers today
}

type nextResult struct {
	Card     *studyCard  `json:"card"`
	Counts   queueCounts `json:"counts"`
	ComeBack string      `json:"come_back,omitempty"` // when the next learning card is due (nothing left before)
	Deck     *store.Deck `json:"deck,omitempty"`
}

// decksWithCounts loads the person's decks (all, or one) with today's numbers;
// "study more" extensions raise a deck's limits for today.
func (s *Server) decksWithCounts(ctx context.Context, uid int64, st store.Settings, archived bool) ([]store.Deck, clock, error) {
	ck := s.clockFor(st)
	decks, err := s.store.ListDecks(ctx, uid, archived)
	if err != nil {
		return nil, ck, err
	}
	today := ck.today()
	for i := range decks {
		if v, _, ok := s.store.CacheGet(ctx, extendKey(uid, decks[i].ID, today)); ok {
			var x struct{ New, Reviews int }
			if json.Unmarshal([]byte(v), &x) == nil {
				decks[i].NewPerDay += x.New
				decks[i].ReviewsPerDay += x.Reviews
			}
		}
	}
	err = s.store.DeckCounts(ctx, uid, decks, ck.window())
	return decks, ck, err
}

func extendKey(uid, deck int64, day string) string {
	return fmt.Sprintf("extend:%d:%d:%s", uid, deck, day)
}

// next picks the card to show: a learning card whose time has come, else a
// review or a new card (new ones spread evenly among the reviews, or first,
// or last), else a learning card due within 20 minutes.
func (s *Server) next(ctx context.Context, uid int64, deckID int64, skip []int64) (*nextResult, error) {
	st, _ := s.store.GetSettings(ctx, uid)
	all, ck, err := s.decksWithCounts(ctx, uid, st, deckID > 0)
	if err != nil {
		return nil, err
	}
	var decks []store.Deck
	for _, d := range all {
		if deckID == 0 || d.ID == deckID {
			decks = append(decks, d)
		}
	}
	res := &nextResult{}
	if deckID > 0 {
		if len(decks) == 0 {
			return nil, store.ErrNotFound
		}
		res.Deck = &decks[0]
	}
	var ids, revDecks []int64
	var newDecks []store.Deck
	newToday, revToday := 0, 0
	byID := map[int64]*store.Deck{}
	for i, d := range decks {
		byID[d.ID] = &decks[i]
		if d.Archived {
			continue
		}
		ids = append(ids, d.ID)
		c := d.Counts
		res.Counts.New += c.DueNew
		res.Counts.Learn += c.DueLearn
		res.Counts.Review += c.DueReview
		res.Counts.Done += c.DoneToday
		newToday += c.NewToday
		revToday += c.DoneToday - c.NewToday
		if c.DueReview > 0 {
			revDecks = append(revDecks, d.ID)
		}
		if c.DueNew > 0 {
			newDecks = append(newDecks, d)
		}
	}
	if len(ids) == 0 {
		return res, nil
	}
	sc := store.Scope{Decks: ids, Today: ck.today(), LearnAhead: store.TS(ck.now), DayEnd: store.TS(ck.dayEnd())}
	var card *store.Card
	if c, err := s.store.NextLearning(ctx, uid, sc, skip); err == nil {
		card = c
	}
	if card == nil {
		pickNew := len(newDecks) > 0
		if pickNew && len(revDecks) > 0 {
			switch st.NewMix {
			case "first":
			case "last":
				pickNew = false
			default:
				nd, nr := float64(newToday), float64(res.Counts.New)
				rd, rr := float64(revToday), float64(res.Counts.Review)
				pickNew = nd/(nd+nr) <= rd/(rd+rr)
			}
		}
		if pickNew {
			for _, d := range newDecks {
				if c, err := s.store.NextNew(ctx, uid, sc, d.ID, d.NewOrder == "random", skip); err == nil {
					card = c
					break
				}
			}
		}
		if card == nil && len(revDecks) > 0 {
			if c, err := s.store.NextReview(ctx, uid, sc, revDecks, skip); err == nil {
				card = c
			}
		}
		if card == nil && !pickNew {
			for _, d := range newDecks {
				if c, err := s.store.NextNew(ctx, uid, sc, d.ID, d.NewOrder == "random", skip); err == nil {
					card = c
					break
				}
			}
		}
	}
	if card == nil {
		sc.LearnAhead = store.TS(ck.now.Add(learnAhead))
		if c, err := s.store.NextLearning(ctx, uid, sc, skip); err == nil {
			card = c
		}
	}
	if card == nil {
		if c, err := s.store.NextLearningLater(ctx, uid, sc); err == nil && c.Due < store.TS(ck.dayEnd()) {
			res.ComeBack = c.Due
		}
		return res, nil
	}
	sc2, err := s.studyCardFor(ctx, uid, st, ck, card.ID, byID[card.DeckID])
	if err != nil {
		return nil, err
	}
	res.Card = sc2
	return res, nil
}

func (s *Server) studyCardFor(ctx context.Context, uid int64, st store.Settings, ck clock, cardID int64, d *store.Deck) (*studyCard, error) {
	b, err := s.store.Brief(ctx, uid, cardID)
	if err != nil {
		return nil, err
	}
	if d == nil {
		d, _ = s.store.GetDeck(ctx, uid, b.DeckID)
	}
	p := s.params(st, d)
	out := &studyCard{CardBrief: b, Answers: s.preview(p, ck, &b.Card)}
	if b.State != store.StateNew {
		out.Retrievability = round2(p.Retrievability(toSRS(&b.Card), float64(ck.elapsedDays(store.ParseTS(b.LastReview)))))
	}
	return out, nil
}

func round2(f float64) float64 { return float64(int(f*100+0.5)) / 100 }

func (s *Server) handleStudyNext(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var skip []int64
	if id := int64(queryInt(r, "skip")); id > 0 {
		skip = []int64{id}
	}
	res, err := s.next(ctx, actorID(ctx), int64(queryInt(r, "deck")), skip)
	if err != nil {
		s.fail(w, err, "next card")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// handleAnswer schedules a card after an answer and returns the next one.
func (s *Server) handleAnswer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in struct {
		CardID     int64 `json:"card_id"`
		Rating     int   `json:"rating"`
		DurationMS int   `json:"duration_ms"`
		Deck       int64 `json:"deck"` // the scope of the session, for the next card
	}
	if err := decode(r, &in); err != nil || in.Rating < 1 || in.Rating > 4 {
		writeErr(w, http.StatusBadRequest, "rating is 1 (again) to 4 (easy)")
		return
	}
	rev, card, err := s.answer(ctx, uid, in.CardID, in.Rating, in.DurationMS)
	if err != nil {
		s.fail(w, err, "answer")
		return
	}
	res, err := s.next(ctx, uid, in.Deck, nil)
	if err != nil {
		s.fail(w, err, "next card")
		return
	}
	if res.Card == nil && res.ComeBack == "" && in.Deck == 0 {
		s.allDone(ctx, uid)
	}
	writeJSON(w, http.StatusOK, map[string]any{"review_id": rev.ID, "card": card, "next": res})
}

// answer applies a rating to one of the person's cards.
func (s *Server) answer(ctx context.Context, uid, cardID int64, rating, durationMS int) (*store.Review, *store.Card, error) {
	st, _ := s.store.GetSettings(ctx, uid)
	c, err := s.store.GetCard(ctx, uid, cardID)
	if err != nil {
		return nil, nil, err
	}
	if c.Suspended {
		return nil, nil, errors.New("this card is set aside")
	}
	d, err := s.store.GetDeck(ctx, uid, c.DeckID)
	if err != nil {
		return nil, nil, err
	}
	ck := s.clockFor(st)
	p := s.params(st, d)
	prev, _ := json.Marshal(c)
	sc := toSRS(c)
	el := ck.elapsedDays(sc.LastReview)
	ret := 0.0
	if c.State != store.StateNew {
		ret = p.Retrievability(sc, float64(el))
	}
	o := p.Review(sc, srs.Rating(rating), ck.now, el, seedOf(c))
	if durationMS < 0 {
		durationMS = 0
	}
	if durationMS > 120000 {
		durationMS = 120000 // a card left open over lunch counts two minutes
	}
	rev := &store.Review{CardID: c.ID, DeckID: c.DeckID, Rating: rating, State: c.State, ElapsedDays: el, LastInterval: c.Interval,
		Interval: o.IntervalDays, Stability: o.Card.Stability, Difficulty: o.Card.Difficulty, Retrievability: round2(ret),
		DurationMS: durationMS, ReviewedAt: store.TS(ck.now), Day: ck.today(), Prev: string(prev)}
	before := c.State
	c.State, c.Step, c.Stability, c.Difficulty = int(o.Card.State), o.Card.Step, o.Card.Stability, o.Card.Difficulty
	c.Due, c.LastReview, c.Reps, c.Lapses = store.TS(o.Card.Due), store.TS(ck.now), o.Card.Reps, o.Card.Lapses
	c.Interval = o.IntervalDays
	c.BuriedUntil = ""
	if err := s.store.Answer(ctx, uid, c, rev); err != nil {
		return nil, nil, err
	}
	s.answers.Add(1)
	if before == store.StateNew || before == store.StateReview {
		_ = s.store.BurySiblings(ctx, uid, c, store.AddDays(ck.today(), 1))
	}
	return rev, c, nil
}

// handleUndo takes the last answer back and shows that card again.
func (s *Server) handleUndo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in struct {
		ReviewID int64 `json:"review_id"`
		Deck     int64 `json:"deck"`
	}
	if err := decode(r, &in); err != nil || in.ReviewID == 0 {
		writeErr(w, http.StatusBadRequest, "review_id is required")
		return
	}
	c, err := s.store.Undo(ctx, uid, in.ReviewID)
	if errors.Is(err, store.ErrNotLast) {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		s.fail(w, err, "undo")
		return
	}
	res, err := s.next(ctx, uid, in.Deck, nil)
	if err != nil {
		s.fail(w, err, "next card")
		return
	}
	st, _ := s.store.GetSettings(ctx, uid)
	if res.Card, err = s.studyCardFor(ctx, uid, st, s.clockFor(st), c.ID, nil); err != nil {
		s.fail(w, err, "card")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// handleFinish ends a session: it is published (Pocket Day, Flows) and returns a summary.
func (s *Server) handleFinish(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in struct {
		Deck       int64 `json:"deck"`
		Cards      int   `json:"cards"`
		Again      int   `json:"again"`
		DurationMS int   `json:"duration_ms"`
	}
	_ = decode(r, &in)
	if in.Cards > 0 {
		deck := "all decks"
		if in.Deck > 0 {
			if d, err := s.store.GetDeck(ctx, uid, in.Deck); err == nil {
				deck = d.Name
			}
		}
		s.publish(ctx, uid, "learn.session_finished", fmt.Sprintf("Studied %d cards (%s)", in.Cards, deck), map[string]any{
			"cards": in.Cards, "again": in.Again, "minutes": round2(float64(in.DurationMS) / 60000), "deck": deck, "deck_id": in.Deck})
	}
	st, _ := s.store.GetSettings(ctx, uid)
	cur, best := s.streaks(ctx, uid, st)
	writeJSON(w, http.StatusOK, map[string]any{"streak": cur, "longest_streak": best})
}

// allDone publishes once a day that everything due today is studied.
func (s *Server) allDone(ctx context.Context, uid int64) {
	st, _ := s.store.GetSettings(ctx, uid)
	ck := s.clockFor(st)
	key := fmt.Sprintf("alldone:%d:%s", uid, ck.today())
	if _, _, done := s.store.CacheGet(ctx, key); done {
		return
	}
	_ = s.store.CachePut(ctx, key, "1")
	cur, _ := s.streaks(ctx, uid, st)
	s.publish(ctx, uid, "learn.all_done", "Everything due today is studied", map[string]any{"day": ck.today(), "streak": cur})
	s.streakMilestone(ctx, uid, st, cur)
}

// streaks are the current and the longest run of study days.
func (s *Server) streaks(ctx context.Context, uid int64, st store.Settings) (int, int) {
	days, err := s.store.StudyDays(ctx, uid)
	if err != nil {
		return 0, 0
	}
	return streakOf(days, s.clockFor(st).today())
}

// streakOf counts runs of consecutive days (newest first); today not yet studied keeps yesterday's streak alive.
func streakOf(days []string, today string) (int, int) {
	if len(days) == 0 {
		return 0, 0
	}
	cur := 0
	if days[0] == today || days[0] == store.AddDays(today, -1) {
		cur = 1
		for i := 1; i < len(days) && days[i] == store.AddDays(days[i-1], -1); i++ {
			cur++
		}
	}
	best, run := 1, 1
	for i := 1; i < len(days); i++ {
		if days[i] == store.AddDays(days[i-1], -1) {
			run++
		} else {
			run = 1
		}
		best = max(best, run)
	}
	return cur, max(best, cur)
}

// ---- card actions ----

func (s *Server) handleCardAction(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	c, err := s.store.GetCard(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "card")
		return
	}
	st, _ := s.store.GetSettings(ctx, uid)
	ck := s.clockFor(st)
	var in struct {
		Action string `json:"action"` // bury | unbury | suspend | unsuspend | forget | flag
		Flag   int    `json:"flag"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	switch in.Action {
	case "bury":
		c.BuriedUntil = store.AddDays(ck.today(), 1)
		err = s.store.SaveCard(ctx, uid, c)
	case "unbury":
		c.BuriedUntil = ""
		err = s.store.SaveCard(ctx, uid, c)
	case "suspend", "unsuspend":
		c.Suspended = in.Action == "suspend"
		err = s.store.SaveCard(ctx, uid, c)
	case "flag":
		if in.Flag < 0 || in.Flag > 4 {
			writeErr(w, http.StatusBadRequest, "flag is 0 to 4")
			return
		}
		c.Flag = in.Flag
		err = s.store.SaveCard(ctx, uid, c)
	case "forget":
		_, err = s.store.ForgetCards(ctx, uid, []int64{c.ID})
	default:
		writeErr(w, http.StatusBadRequest, "unknown action")
		return
	}
	if err != nil {
		s.fail(w, err, "card")
		return
	}
	c, _ = s.store.GetCard(ctx, uid, c.ID)
	writeJSON(w, http.StatusOK, c)
}

// handleCardHistory is one card's answers and its memory over time.
func (s *Server) handleCardHistory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	c, err := s.store.GetCard(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "card")
		return
	}
	revs, err := s.store.CardReviews(ctx, uid, c.ID)
	if err != nil {
		s.fail(w, err, "history")
		return
	}
	st, _ := s.store.GetSettings(ctx, uid)
	ck := s.clockFor(st)
	d, _ := s.store.GetDeck(ctx, uid, c.DeckID)
	p := s.params(st, d)
	ret := 0.0
	if c.State != store.StateNew {
		ret = round2(p.Retrievability(toSRS(c), float64(ck.elapsedDays(store.ParseTS(c.LastReview)))))
	}
	writeJSON(w, http.StatusOK, map[string]any{"card": c, "reviews": revs, "retrievability": ret})
}

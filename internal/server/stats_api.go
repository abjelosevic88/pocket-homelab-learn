package server

import (
	"context"
	"net/http"
	"time"

	"github.com/abjelosevic88/pocket-homelab-learn/internal/store"
)

// dashboard is today at a glance: the sidebar, Homepage widgets, Pocket Hub.
type dashboard struct {
	Due           int    `json:"due"`
	DueNew        int    `json:"due_new"`
	DueLearn      int    `json:"due_learn"`
	DueReview     int    `json:"due_review"`
	DoneToday     int    `json:"done_today"`
	MinutesToday  int    `json:"minutes_today"`
	Goal          int    `json:"goal"`
	Streak        int    `json:"streak"`
	LongestStreak int    `json:"longest_streak"`
	DueTomorrow   int    `json:"due_tomorrow"`
	Cards         int    `json:"cards"`
	Decks         int    `json:"decks"`
	Today         string `json:"today"`
	ComeBack      string `json:"come_back,omitempty"`
}

func (s *Server) dashboardFor(ctx context.Context, uid int64) (*dashboard, []store.Deck, error) {
	st, _ := s.store.GetSettings(ctx, uid)
	decks, ck, err := s.decksWithCounts(ctx, uid, st, false)
	if err != nil {
		return nil, nil, err
	}
	d := &dashboard{Today: ck.today(), Goal: st.DailyGoal, Decks: len(decks)}
	for _, x := range decks {
		c := x.Counts
		d.DueNew += c.DueNew
		d.DueLearn += c.DueLearn
		d.DueReview += c.DueReview
		d.DoneToday += c.DoneToday
		d.DueTomorrow += c.DueTomorow
		d.Cards += c.Total
	}
	d.Due = d.DueNew + d.DueLearn + d.DueReview
	if days, err := s.store.DayStats(ctx, uid, d.Today, d.Today, 0); err == nil && len(days) > 0 {
		d.MinutesToday = int(days[0].DurationMS/60000 + 0)
	}
	d.Streak, d.LongestStreak = s.streaks(ctx, uid, st)
	if d.Due == 0 {
		if res, err := s.next(ctx, uid, 0, nil); err == nil {
			d.ComeBack = res.ComeBack
			if res.Card != nil {
				d.Due = 1
			}
		}
	}
	return d, decks, nil
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	d, _, err := s.dashboardFor(ctx, actorID(ctx))
	if err != nil {
		s.fail(w, err, "dashboard")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// handleHome is the Today page: the dashboard, the decks and the next days.
func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	d, decks, err := s.dashboardFor(ctx, uid)
	if err != nil {
		s.fail(w, err, "home")
		return
	}
	st, _ := s.store.GetSettings(ctx, uid)
	ck := s.clockFor(st)
	from := store.AddDays(ck.today(), -83)
	days, _ := s.store.DayStats(ctx, uid, from, ck.today(), 0)
	heat := map[string]int{}
	for _, x := range days {
		heat[x.Day] = x.Reviews
	}
	fc, _ := s.forecast(ctx, uid, ck, 7)
	today := store.LocalDate(s.now())
	_, n, secs := s.sessionsList(ctx, uid, today, today, 0, 0)
	writeJSON(w, http.StatusOK, map[string]any{"dashboard": d, "decks": decks, "heat": heat, "heat_from": from, "forecast": fc,
		"sessions_today": map[string]any{"count": n, "seconds": secs}})
}

// forecast counts the cards due on each of the next n study days (today includes overdue ones).
func (s *Server) forecast(ctx context.Context, uid int64, ck clock, n int) ([]int, error) {
	until := ck.dayStart(store.AddDays(ck.today(), n))
	dues, err := s.store.DueTimes(ctx, uid, store.TS(until))
	if err != nil {
		return nil, err
	}
	out := make([]int, n)
	today, _ := store.ParseDate(ck.today())
	for _, d := range dues {
		t := store.ParseTS(d)
		if t.IsZero() {
			continue
		}
		day, _ := store.ParseDate(ck.day(t))
		i := int(day.Sub(today).Hours()/24 + 0.5)
		if i < 0 {
			i = 0
		}
		if i < n {
			out[i]++
		}
	}
	return out, nil
}

func rangeDays(rng string) int {
	switch rng {
	case "w":
		return 7
	case "m":
		return 30
	case "3m":
		return 91
	case "y":
		return 365
	case "all":
		return 3650
	}
	return 30
}

// handleStats is everything on the Statistics page.
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	st, _ := s.store.GetSettings(ctx, uid)
	ck := s.clockFor(st)
	n := rangeDays(r.URL.Query().Get("range"))
	deck := int64(queryInt(r, "deck"))
	to := ck.today()
	from := store.AddDays(to, -(n - 1))
	life, _ := s.store.LifetimeTotals(ctx, uid)
	if r.URL.Query().Get("range") == "all" && life.First != "" && life.First > from {
		from = life.First
	}
	days, err := s.store.DayStats(ctx, uid, from, to, deck)
	if err != nil {
		s.fail(w, err, "stats")
		return
	}
	// every day of the range, zeros included
	byDay := map[string]store.DayStat{}
	for _, d := range days {
		byDay[d.Day] = d
	}
	series := []store.DayStat{}
	var sum store.DayStat
	studied := 0
	for d := from; d <= to; d = store.AddDays(d, 1) {
		x := byDay[d]
		x.Day = d
		series = append(series, x)
		sum.Reviews += x.Reviews
		sum.New += x.New
		sum.Learn += x.Learn
		sum.Review += x.Review
		sum.Again += x.Again
		sum.Mature += x.Mature
		sum.MatureOK += x.MatureOK
		sum.DurationMS += x.DurationMS
		if x.Reviews > 0 {
			studied++
		}
	}
	heatFrom := store.AddDays(to, -364)
	hdays, _ := s.store.DayStats(ctx, uid, heatFrom, to, deck)
	heat := map[string]int{}
	for _, x := range hdays {
		heat[x.Day] = x.Reviews
	}
	decks, _, _ := s.decksWithCounts(ctx, uid, st, true)
	var counts store.DeckCounts
	for _, d := range decks {
		if deck > 0 && d.ID != deck {
			continue
		}
		c := d.Counts
		counts.Total += c.Total
		counts.New += c.New
		counts.Learning += c.Learning
		counts.Young += c.Young
		counts.Mature += c.Mature
		counts.Suspended += c.Suspended
	}
	fc, _ := s.forecast(ctx, uid, ck, 30)
	ivals, _ := s.store.IntervalCounts(ctx, uid)
	buckets := []struct {
		Label string `json:"label"`
		Lo    int    `json:"-"`
		Hi    int    `json:"-"`
		N     int    `json:"count"`
	}{{"1 day", 1, 1, 0}, {"2–6 days", 2, 6, 0}, {"1–2 weeks", 7, 13, 0}, {"2–3 weeks", 14, 20, 0}, {"3 weeks–2 months", 21, 59, 0}, {"2–6 months", 60, 179, 0}, {"6–12 months", 180, 364, 0}, {"a year +", 365, 1 << 30, 0}}
	for iv, c := range ivals {
		for i := range buckets {
			if iv >= buckets[i].Lo && iv <= buckets[i].Hi {
				buckets[i].N += c
			}
		}
	}
	// answers by hour of the day, and how often a review was remembered
	recent, _ := s.store.RecentReviews(ctx, uid, store.TS(ck.dayStart(from)))
	hours := make([]store.HourStat, 24)
	for i := range hours {
		hours[i].Hour = i
	}
	for _, rv := range recent {
		if deck > 0 && rv.DeckID != deck {
			continue
		}
		t := store.ParseTS(rv.ReviewedAt).In(time.Local)
		h := &hours[t.Hour()]
		h.Reviews++
		if rv.State == store.StateReview {
			h.Total++
			if rv.Rating > 1 {
				h.OK++
			}
		}
	}
	cur, best := s.streaks(ctx, uid, st)
	hard, _ := s.store.HardCards(ctx, uid, 4, 10)
	retention := func(ok, all int) any {
		if all == 0 {
			return nil
		}
		return round2(float64(ok) / float64(all))
	}
	young, youngOK := sum.Review-sum.Mature, (sum.Review-sum.Again)-sum.MatureOK
	sessTo := to // sessions go by calendar date; the study day may still be yesterday's
	if d := store.LocalDate(s.now()); d > sessTo {
		sessTo = d
	}
	sessions, sessionCount, sessionSecs := s.sessionsList(ctx, uid, from, sessTo, deck, 12)
	writeJSON(w, http.StatusOK, map[string]any{
		"from": from, "to": to, "days": series, "heat": heat, "heat_from": heatFrom,
		"totals": map[string]any{"reviews": sum.Reviews, "new": sum.New, "minutes": int(sum.DurationMS / 60000), "days_studied": studied, "days": len(series),
			"per_day": round2(float64(sum.Reviews) / float64(max(1, len(series)))), "seconds_per_card": round2(float64(sum.DurationMS) / 1000 / float64(max(1, sum.Reviews)))},
		"retention":         retention(sum.Review-sum.Again, sum.Review),
		"retention_young":   retention(youngOK, young),
		"retention_mature":  retention(sum.MatureOK, sum.Mature),
		"desired_retention": st.Retention,
		"counts":            counts, "forecast": fc, "intervals": buckets, "hours": hours,
		"streak": cur, "longest_streak": best, "lifetime": life, "hard": hard,
		"sessions": sessions, "session_totals": map[string]any{"count": sessionCount, "seconds": sessionSecs},
	})
}

// handleDay is one study day for Pocket Day's journal: how much, which decks, how well.
func (s *Server) handleDay(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	date := chiParam(r, "date")
	if !validDate(date) {
		writeErr(w, http.StatusBadRequest, "date is YYYY-MM-DD")
		return
	}
	days, err := s.store.DayStats(ctx, uid, date, date, 0)
	if err != nil {
		s.fail(w, err, "day")
		return
	}
	per, _ := s.store.DayDeckCounts(ctx, uid, date)
	decks, _ := s.store.ListDecks(ctx, uid, true)
	type deckDay struct {
		ID      int64  `json:"id"`
		Name    string `json:"name"`
		Reviews int    `json:"reviews"`
		New     int    `json:"new"`
	}
	list := []deckDay{}
	for _, d := range decks {
		if c, ok := per[d.ID]; ok {
			list = append(list, deckDay{d.ID, d.Name, c.All, c.New})
		}
	}
	out := map[string]any{"date": date, "reviews": 0, "new": 0, "minutes": 0, "decks": list, "retention": nil}
	if len(days) > 0 {
		d := days[0]
		out["reviews"], out["new"], out["minutes"] = d.Reviews, d.New, round2(float64(d.DurationMS)/60000)
		if d.Review > 0 {
			out["retention"] = round2(float64(d.Review-d.Again) / float64(d.Review))
		}
	}
	writeJSON(w, http.StatusOK, out)
}

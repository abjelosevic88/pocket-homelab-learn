package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-learn/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-learn/internal/store"
)

// pushes runs every minute and sends, each at the person's own time: the
// study reminder (only when enough cards are due), the evening "your streak
// ends tonight" nudge and the weekly report.
func (s *Server) pushes(ctx context.Context) {
	ids, err := s.store.SettingsUsers(ctx)
	if err != nil {
		return
	}
	now := s.now()
	hm := now.Format("15:04")
	wd := int(now.Weekday())
	if wd == 0 {
		wd = 7
	}
	for _, uid := range ids {
		st, err := s.store.GetSettings(ctx, uid)
		if err != nil || st.NotifyURL == "" {
			continue
		}
		today := s.clockFor(st).today()
		n := notify.New(st.NotifyURL, st.NotifyFormat)
		once := func(key string) bool {
			k := fmt.Sprintf("push:%d:%s:%s", uid, today, key)
			if _, _, done := s.store.CacheGet(ctx, k); done {
				return false
			}
			_ = s.store.CachePut(ctx, k, "1")
			return true
		}
		remind := st.ReminderTime != "" && strings.Contains(st.ReminderDays, fmt.Sprint(wd)) && withinMinutes(hm, st.ReminderTime, 4)
		streak := st.NotifyStreak && now.Hour() == st.StreakHour && now.Minute() < 5
		weekly := st.NotifyWeekly && wd == st.WeeklyDay && now.Hour() == st.WeeklyHour && now.Minute() < 5
		if !remind && !streak && !weekly {
			continue
		}
		d, _, err := s.dashboardFor(ctx, uid)
		if err != nil {
			continue
		}
		if remind && d.Due >= max(1, st.ReminderMin) && once("reminder") {
			_ = n.Send(ctx, notify.Message{Title: dueTitle(d), Body: dueBody(d), Priority: "info", Tags: []string{"brain"}, Link: s.cfg.BaseURL + "/study"})
		}
		if streak && d.DoneToday == 0 && d.Streak >= 2 && d.Due > 0 && once("streak") {
			_ = n.Send(ctx, notify.Message{Title: fmt.Sprintf("Keep your %d-day streak", d.Streak), Body: fmt.Sprintf("Nothing studied yet today; %s waiting. A few minutes keep it going.", plural(d.Due, "card is", "cards are")), Priority: "warning", Tags: []string{"fire"}, Link: s.cfg.BaseURL + "/study"})
		}
		if weekly && once("weekly") {
			if msg := s.weeklyReport(ctx, uid, st); msg != "" {
				_ = n.Send(ctx, notify.Message{Title: "Your week in Pocket Learn", Body: msg, Priority: "info", Tags: []string{"bar_chart"}, Link: s.cfg.BaseURL + "/stats"})
			}
		}
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

func dueTitle(d *dashboard) string { return plural(d.Due, "card to study", "cards to study") }

func dueBody(d *dashboard) string {
	parts := []string{}
	if d.DueReview > 0 {
		parts = append(parts, plural(d.DueReview, "review", "reviews"))
	}
	if d.DueLearn > 0 {
		parts = append(parts, fmt.Sprintf("%d in learning", d.DueLearn))
	}
	if d.DueNew > 0 {
		parts = append(parts, plural(d.DueNew, "new card", "new cards"))
	}
	line := strings.Join(parts, ", ") + "."
	if d.Streak >= 2 {
		line += fmt.Sprintf(" Day %d of your streak.", d.Streak+1)
	}
	return line
}

// withinMinutes reports whether now (HH:MM) is at or up to n minutes after t (HH:MM).
func withinMinutes(now, t string, n int) bool {
	a, err1 := time.Parse("15:04", now)
	b, err2 := time.Parse("15:04", t)
	if err1 != nil || err2 != nil {
		return false
	}
	d := a.Sub(b)
	return d >= 0 && d <= time.Duration(n)*time.Minute
}

// weeklyReport is the plain-text summary of the last seven days vs the seven before.
func (s *Server) weeklyReport(ctx context.Context, uid int64, st store.Settings) string {
	ck := s.clockFor(st)
	to := ck.today()
	from := store.AddDays(to, -6)
	days, err := s.store.DayStats(ctx, uid, from, to, 0)
	if err != nil || len(days) == 0 {
		return ""
	}
	prev, _ := s.store.DayStats(ctx, uid, store.AddDays(from, -7), store.AddDays(from, -1), 0)
	var sum store.DayStat
	for _, d := range days {
		sum.Reviews += d.Reviews
		sum.New += d.New
		sum.Review += d.Review
		sum.Again += d.Again
		sum.DurationMS += d.DurationMS
	}
	prevN := 0
	for _, d := range prev {
		prevN += d.Reviews
	}
	lines := []string{fmt.Sprintf("%d answers on %d of 7 days, %d minutes", sum.Reviews, len(days), sum.DurationMS/60000)}
	if prevN > 0 {
		lines[0] += fmt.Sprintf(" (%+.0f%% vs the week before)", float64(sum.Reviews-prevN)/float64(prevN)*100)
	}
	if sum.New > 0 {
		lines = append(lines, plural(sum.New, "new card learned", "new cards learned"))
	}
	if sum.Review > 0 {
		lines = append(lines, fmt.Sprintf("Remembered %.0f%% of your reviews", float64(sum.Review-sum.Again)/float64(sum.Review)*100))
	}
	if cur, _ := s.streaks(ctx, uid, st); cur >= 2 {
		lines = append(lines, fmt.Sprintf("Streak: %d days", cur))
	}
	if fc, err := s.forecast(ctx, uid, ck, 8); err == nil {
		next := 0
		for _, n := range fc[1:] {
			next += n
		}
		lines = append(lines, fmt.Sprintf("Coming up in the next 7 days: %d reviews", next))
	}
	return strings.Join(lines, "\n")
}

// streakMilestone pushes (and publishes) 7, 30, 100 and 365 days in a row, once each.
func (s *Server) streakMilestone(ctx context.Context, uid int64, st store.Settings, streak int) {
	for _, m := range []int{7, 30, 100, 200, 365, 500, 1000} {
		if streak != m {
			continue
		}
		key := fmt.Sprintf("milestone:%d:streak:%d:%s", uid, m, s.clockFor(st).today())
		if _, _, done := s.store.CacheGet(ctx, key); done {
			return
		}
		_ = s.store.CachePut(ctx, key, "1")
		s.publish(ctx, uid, "learn.streak_reached", fmt.Sprintf("%d days in a row", m), map[string]any{"days": m})
		if st.NotifyMilestones && st.NotifyURL != "" {
			_ = notify.New(st.NotifyURL, st.NotifyFormat).Send(ctx, notify.Message{Title: fmt.Sprintf("%d days in a row", m), Body: "Every day for " + plural(m, "day", "days") + ". That is how things stay learned.", Priority: "info", Tags: []string{"trophy"}, Link: s.cfg.BaseURL + "/stats"})
		}
	}
}

// handleWeeklyNow sends the weekly report right away (Settings → Reminders).
func (s *Server) handleWeeklyNow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	st, _ := s.store.GetSettings(ctx, uid)
	msg := s.weeklyReport(ctx, uid, st)
	if msg == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "nothing studied in the last seven days yet"})
		return
	}
	if st.NotifyURL == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "sent": false, "report": msg})
		return
	}
	n := notify.New(st.NotifyURL, st.NotifyFormat)
	if err := n.Send(ctx, notify.Message{Title: "Your week in Pocket Learn", Body: msg, Priority: "info", Tags: []string{"bar_chart"}, Link: s.cfg.BaseURL + "/stats"}); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error(), "report": msg})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "sent": true, "report": msg})
}

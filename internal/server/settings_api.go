package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-learn/internal/ai"
	"github.com/abjelosevic88/pocket-homelab-learn/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-learn/internal/pocket"
	"github.com/abjelosevic88/pocket-homelab-learn/internal/srs"
	"github.com/abjelosevic88/pocket-homelab-learn/internal/store"
)

// ---- settings ----

func mask(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return "••••"
	}
	return s[:4] + "••••" + s[len(s)-4:]
}

// publicSettings is what one person sees: their settings with the secrets
// masked and, per Pocket app, the mode in effect (<app>_mode), whether it
// works and how (<app>_via: core | direct | "") and why not (<app>_detail).
func (s *Server) publicSettings(ctx context.Context, u *store.User, st store.Settings) map[string]any {
	out := map[string]any{
		"core_configured": s.core.Configured(), "core_registered": s.coreStatus()["registered"],
		"title": st.Title, "date_format": st.DateFormat, "time_format": st.TimeFormat, "week_start": st.WeekStart,
		"day_start_hour": st.DayStartHour, "retention": st.Retention, "learning_steps": st.LearningSteps, "relearning_steps": st.RelearningSteps,
		"max_interval": st.MaxInterval, "new_per_day": st.NewPerDay, "reviews_per_day": st.ReviewsPerDay, "new_mix": st.NewMix, "daily_goal": st.DailyGoal,
		"show_intervals": st.ShowIntervals, "show_timer": st.ShowTimer, "autoplay": st.Autoplay, "speak": st.Speak, "speak_lang": st.SpeakLang,
		"reminder_time": st.ReminderTime, "reminder_days": st.ReminderDays, "reminder_min": st.ReminderMin,
		"notify_streak": st.NotifyStreak, "streak_hour": st.StreakHour, "notify_weekly": st.NotifyWeekly, "weekly_day": st.WeeklyDay, "weekly_hour": st.WeeklyHour,
		"notify_milestones": st.NotifyMilestones, "notify_url": st.NotifyURL, "notify_format": st.NotifyFormat, "notify_configured": st.NotifyURL != "",
		"reader_url": st.ReaderURL, "reader_token": mask(st.ReaderToken), "reader_public_url": st.ReaderPublicURL,
		"library_url": st.LibraryURL, "library_token": mask(st.LibraryToken), "library_public_url": st.LibraryPublicURL,
		"notes_url": st.NotesURL, "notes_token": mask(st.NotesToken), "notes_public_url": st.NotesPublicURL,
		"time_url": st.TimeURL, "time_token": mask(st.TimeToken), "time_public_url": st.TimePublicURL,
		"ai_provider": st.AIProvider, "ai_pocket_model": st.AIPocketModel, "openai_api_key": mask(st.OpenAIKey), "openai_model": st.OpenAIModel, "openai_base_url": st.OpenAIBaseURL,
		"setup_complete": st.SetupComplete, "base_url": s.cfg.BaseURL,
	}
	for _, a := range pocketApps {
		mode, via, detail := s.connStatus(ctx, u, st, a)
		out[a.slug+"_mode"], out[a.slug+"_via"], out[a.slug+"_detail"] = mode, via, detail
		out[a.slug+"_configured"] = via != ""
		_, _, _, public := connSettings(st, a)
		if public == "" && via != "" {
			public = s.resolveSibling(ctx, a.slug)
		}
		out[a.slug+"_open_url"] = public
	}
	return out
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.GetSettings(r.Context(), actorID(r.Context()))
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	writeJSON(w, http.StatusOK, s.publicSettings(r.Context(), userFrom(r.Context()), st))
}

var hhmm = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

// handlePutSettings saves the caller's personal settings. The box-wide title
// is only taken from an admin.
func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	me := userFrom(r.Context())
	isAdmin := me.Role == "admin"
	cur, err := s.store.GetSettings(r.Context(), me.ID)
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	var in store.Settings
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	bad := func(msg string) { writeErr(w, http.StatusBadRequest, msg) }
	if keys["title"] && isAdmin {
		cur.Title = strings.TrimSpace(in.Title)
		if cur.Title == "" {
			cur.Title = store.DefaultTitle
		}
	}
	if keys["date_format"] {
		cur.DateFormat = in.DateFormat
	}
	if keys["time_format"] {
		cur.TimeFormat = in.TimeFormat
	}
	if keys["week_start"] && (in.WeekStart == 1 || in.WeekStart == 7 || in.WeekStart == 6) {
		cur.WeekStart = in.WeekStart
	}
	intIn := func(k string, dst *int, v, lo, hi int) bool {
		if !keys[k] {
			return true
		}
		if v < lo || v > hi {
			bad(fmt.Sprintf("%s is %d to %d", k, lo, hi))
			return false
		}
		*dst = v
		return true
	}
	if !intIn("day_start_hour", &cur.DayStartHour, in.DayStartHour, 0, 12) || !intIn("max_interval", &cur.MaxInterval, in.MaxInterval, 1, 36500) ||
		!intIn("new_per_day", &cur.NewPerDay, in.NewPerDay, 0, 9999) || !intIn("reviews_per_day", &cur.ReviewsPerDay, in.ReviewsPerDay, 0, 99999) ||
		!intIn("daily_goal", &cur.DailyGoal, in.DailyGoal, 0, 9999) || !intIn("reminder_min", &cur.ReminderMin, in.ReminderMin, 1, 9999) ||
		!intIn("streak_hour", &cur.StreakHour, in.StreakHour, 0, 23) || !intIn("weekly_day", &cur.WeeklyDay, in.WeeklyDay, 1, 7) ||
		!intIn("weekly_hour", &cur.WeeklyHour, in.WeeklyHour, 0, 23) {
		return
	}
	if keys["retention"] {
		if in.Retention < 0.7 || in.Retention > 0.99 {
			bad("retention is 70% to 99%")
			return
		}
		cur.Retention = in.Retention
	}
	steps := func(k string, dst *string, v string) bool {
		if !keys[k] {
			return true
		}
		d, err := srs.ParseSteps(v)
		if err != nil {
			bad(err.Error())
			return false
		}
		*dst = srs.FormatSteps(d)
		return true
	}
	if !steps("learning_steps", &cur.LearningSteps, in.LearningSteps) || !steps("relearning_steps", &cur.RelearningSteps, in.RelearningSteps) {
		return
	}
	if keys["new_mix"] {
		if in.NewMix != "mix" && in.NewMix != "first" && in.NewMix != "last" {
			bad("new cards come mixed in, first or last")
			return
		}
		cur.NewMix = in.NewMix
	}
	if keys["reminder_time"] {
		t := strings.TrimSpace(in.ReminderTime)
		if t != "" && !hhmm.MatchString(t) {
			bad("the reminder time is HH:MM")
			return
		}
		cur.ReminderTime = t
	}
	if keys["reminder_days"] {
		d := ""
		for _, c := range "1234567" {
			if strings.ContainsRune(in.ReminderDays, c) {
				d += string(c)
			}
		}
		cur.ReminderDays = d
	}
	secret := func(k string, dst *string, v string) {
		if keys[k] && !strings.Contains(v, "••") {
			*dst = strings.TrimSpace(v)
		}
	}
	url := func(k string, dst *string, v string) {
		if keys[k] {
			*dst = strings.TrimRight(strings.TrimSpace(v), "/")
		}
	}
	text := func(k string, dst *string, v string) {
		if keys[k] {
			*dst = strings.TrimSpace(v)
		}
	}
	boolIn := func(k string, dst *bool, v bool) {
		if keys[k] {
			*dst = v
		}
	}
	boolIn("show_intervals", &cur.ShowIntervals, in.ShowIntervals)
	boolIn("show_timer", &cur.ShowTimer, in.ShowTimer)
	boolIn("autoplay", &cur.Autoplay, in.Autoplay)
	boolIn("speak", &cur.Speak, in.Speak)
	text("speak_lang", &cur.SpeakLang, in.SpeakLang)
	boolIn("notify_streak", &cur.NotifyStreak, in.NotifyStreak)
	boolIn("notify_weekly", &cur.NotifyWeekly, in.NotifyWeekly)
	boolIn("notify_milestones", &cur.NotifyMilestones, in.NotifyMilestones)
	text("notify_url", &cur.NotifyURL, in.NotifyURL)
	if keys["notify_format"] && in.NotifyFormat != "" {
		cur.NotifyFormat = in.NotifyFormat
	}
	url("reader_url", &cur.ReaderURL, in.ReaderURL)
	secret("reader_token", &cur.ReaderToken, in.ReaderToken)
	url("reader_public_url", &cur.ReaderPublicURL, in.ReaderPublicURL)
	url("library_url", &cur.LibraryURL, in.LibraryURL)
	secret("library_token", &cur.LibraryToken, in.LibraryToken)
	url("library_public_url", &cur.LibraryPublicURL, in.LibraryPublicURL)
	url("notes_url", &cur.NotesURL, in.NotesURL)
	secret("notes_token", &cur.NotesToken, in.NotesToken)
	url("notes_public_url", &cur.NotesPublicURL, in.NotesPublicURL)
	url("time_url", &cur.TimeURL, in.TimeURL)
	secret("time_token", &cur.TimeToken, in.TimeToken)
	url("time_public_url", &cur.TimePublicURL, in.TimePublicURL)
	if keys["ai_provider"] {
		if !validAIProvider(in.AIProvider) {
			bad("AI is automatic, Pocket AI or OpenAI")
			return
		}
		cur.AIProvider = in.AIProvider
	}
	if keys["ai_pocket_model"] {
		if !validPocketModel(in.AIPocketModel) {
			bad("unknown model")
			return
		}
		cur.AIPocketModel = in.AIPocketModel
	}
	secret("openai_api_key", &cur.OpenAIKey, in.OpenAIKey)
	text("openai_model", &cur.OpenAIModel, in.OpenAIModel)
	url("openai_base_url", &cur.OpenAIBaseURL, in.OpenAIBaseURL)
	mode := func(k string, dst *string, v string) {
		if keys[k] && (v == modeCore || v == modeDirect || v == modeOff) {
			*dst = v
		}
	}
	mode("reader_mode", &cur.ReaderMode, in.ReaderMode)
	mode("library_mode", &cur.LibraryMode, in.LibraryMode)
	mode("notes_mode", &cur.NotesMode, in.NotesMode)
	mode("time_mode", &cur.TimeMode, in.TimeMode)
	if err := s.store.SaveUserSettings(r.Context(), me.ID, cur); err != nil {
		s.fail(w, err, "save settings")
		return
	}
	if isAdmin && keys["title"] {
		if err := s.store.SaveBoxSettings(r.Context(), cur); err != nil {
			s.fail(w, err, "save settings")
			return
		}
	}
	for _, k := range modeKeys {
		if keys[k] {
			s.reportModesSoon()
			break
		}
	}
	writeJSON(w, http.StatusOK, s.publicSettings(r.Context(), me, cur))
}

// handleIntegrationTest checks a connection with the caller's own saved
// settings (or the URL/key typed into the form).
func (s *Server) handleIntegrationTest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Which  string `json:"which"`
		URL    string `json:"url"`
		Key    string `json:"key"`
		User   string `json:"user"`
		Format string `json:"format"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	uid := actorID(r.Context())
	st, _ := s.store.GetSettings(r.Context(), uid)
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	keyOr := func(saved string) string {
		if strings.Contains(in.Key, "••") || in.Key == "" {
			return saved
		}
		return in.Key
	}
	urlOr := func(saved string) string {
		if strings.TrimSpace(in.URL) == "" {
			return saved
		}
		return strings.TrimRight(strings.TrimSpace(in.URL), "/")
	}
	var msg string
	var err error
	if a, ok := appBySlug(in.Which); ok {
		mode, savedURL, savedTok, _ := connSettings(st, a)
		typed := in.Key != "" && !strings.Contains(in.Key, "••")
		var conn *siblingConn
		if tok := keyOr(savedTok); tok != "" && (typed || s.connMode(mode, savedTok) == modeDirect) {
			base := urlOr(savedURL)
			if base == "" {
				base = s.resolveSibling(ctx, a.slug)
			}
			conn = &siblingConn{client: pocket.New(base, tok, 15*time.Second), via: modeDirect}
		} else {
			scopes := a.scopes
			if a.slug == appTime.slug {
				scopes = []string{scopeTimeWrite} // what the timer needs
			}
			conn, err = s.clients(ctx, uid).conn(ctx, a, scopes...)
		}
		if err == nil {
			how := "direct connection"
			if conn.via == modeCore {
				how = "through Pocket Core"
			}
			var v string
			if v, err = conn.client.Health(ctx); err == nil {
				switch a.slug {
				case "reader":
					var hs []pocket.Highlight
					hs, err = conn.client.Highlights(ctx, "", 200)
					msg = fmt.Sprintf("Pocket Reader %s, %s, %d highlights", v, how, len(hs))
				case "library":
					var as []pocket.Annotation
					as, err = conn.client.Annotations(ctx, "", 200)
					msg = fmt.Sprintf("Pocket Library %s, %s, %d highlights and notes", v, how, len(as))
				case "notes":
					var ns []pocket.Note
					ns, err = conn.client.SearchNotes(ctx, "", 50)
					msg = fmt.Sprintf("Pocket Notes %s, %s, %d recent notes", v, how, len(ns))
				case "time":
					var run *pocket.TimeEntry
					if run, err = conn.client.Timer(ctx); err == nil {
						msg = fmt.Sprintf("Pocket Time %s, %s, no timer running", v, how)
						if run != nil {
							msg = fmt.Sprintf("Pocket Time %s, %s, timer running: %s (%s)", v, how, run.Name(), startedIn(run))
						}
					}
				}
			}
		}
	} else if in.Which == "openai" {
		c := ai.New(urlOr(st.OpenAIBaseURL), keyOr(st.OpenAIKey), st.OpenAIModel, 20*time.Second)
		var ids []string
		if ids, err = c.Models(ctx); err == nil {
			msg = fmt.Sprintf("The key works (%d models)", len(ids))
		}
	} else if in.Which == "notify" {
		f := in.Format
		if f == "" {
			f = st.NotifyFormat
		}
		n := notify.New(urlOr(st.NotifyURL), f)
		err = n.Send(ctx, notify.Message{Title: "Pocket Learn", Body: "Notifications work. Study reminders, your streak and the weekly report will arrive here.", Priority: "info", Tags: []string{"brain"}})
		msg = "Test message sent via " + n.Format
	} else {
		writeErr(w, http.StatusBadRequest, "unknown integration")
		return
	}
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": msg})
}

func (s *Server) handleNotifyTest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st, _ := s.store.GetSettings(ctx, actorID(ctx))
	n := notify.New(st.NotifyURL, st.NotifyFormat)
	if err := n.Send(ctx, notify.Message{Title: "Pocket Learn", Body: "Notifications work.", Priority: "info", Tags: []string{"brain"}}); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- system / backup / export ----

func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	var size int64
	if fi, err := os.Stat(s.cfg.DBPath); err == nil {
		size = fi.Size()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version": Version, "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH,
		"core":      s.coreStatus(),
		"auth_mode": s.cfg.AuthMode, "data_dir": s.cfg.DataDir, "db_path": s.cfg.DBPath, "db_bytes": size,
		"uptime": time.Since(s.started).Round(time.Second).String(), "timezone": time.Now().Location().String(),
		"counts": s.store.CountsFor(r.Context(), actorID(r.Context())), "answers": s.answers.Load(),
	})
}

// handleBackup downloads the whole database, an admin-only box operation.
func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	dir := filepath.Join(s.cfg.DataDir, "backups")
	name := fmt.Sprintf("pocket-learn-%s.db", time.Now().Format("20060102-150405"))
	dst := filepath.Join(dir, name)
	if err := s.store.Backup(r.Context(), dst); err != nil {
		s.fail(w, err, "backup")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.sqlite3")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeFile(w, r, dst)
}

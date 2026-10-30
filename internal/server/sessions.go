package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-learn/internal/pocket"
	"github.com/abjelosevic88/pocket-homelab-learn/internal/store"
)

// ---- study sessions and the shared timer ----
//
// A study session is one run on the study screen: it starts when the screen
// has a card to show and ends when the person finishes, leaves the screen,
// presses Stop, or answers nothing for 15 minutes (then it ends at the last
// answer). One run at a time per person: starting another closes the open one.
//
// Without Pocket Time (no Core, link off, Settings → off) Learn records the
// session itself (source "app"). With Pocket Time connected the study timer
// IS the person's one timer there: starting a run starts it (app_ref
// "deck:<id>", deliver true), ending the run stops it (only_if), and Pocket
// Time writes the finished time back through POST /worklog with external_id
// "time:<entry id>" into the same row (matched by timer_entry_id), so the
// time is never there twice. Until then the row is "kept by Pocket Time" and
// is left out of the worklog Learn offers.

const sessionIdle = 15 * time.Minute

// sessionOut is a session for the UI.
type sessionOut struct {
	store.StudySession
	KeptByTime bool `json:"kept_by_time"` // timed in Pocket Time
	ElapsedS   int  `json:"elapsed_s"`    // so far, for an open one
}

func (s *Server) sessionView(x *store.StudySession) *sessionOut {
	if x == nil {
		return nil
	}
	out := &sessionOut{StudySession: *x, KeptByTime: x.TimerEntryID > 0}
	if x.Source == "time" {
		out.KeptByTime = true
	}
	if x.Open() {
		if t, ok := store.ParseStamp(x.StartedAt); ok {
			out.ElapsedS = max(0, int(s.now().Sub(t).Seconds()))
		}
	}
	return out
}

// timerOut is Pocket Time's running timer as Learn shows it.
type timerOut struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	StartedIn string `json:"started_in"` // "Pocket Guitar", "Pocket Time"
	StartedAt string `json:"started_at"`
	App       string `json:"app"`
	AppRef    string `json:"app_ref"`
	Mine      bool   `json:"mine"` // timing one of this person's study runs here
}

// startedIn names the app a timer was started in.
func startedIn(e *pocket.TimeEntry) string {
	switch e.Source {
	case "", "time", "manual", "import":
		return "Pocket Time"
	}
	names := map[string]string{"learn": "Pocket Learn", "guitar": "Pocket Guitar", "projects": "Pocket Projects", "still": "Pocket Still", "invoicing": "Pocket Invoicing"}
	if n, ok := names[e.Source]; ok {
		return n
	}
	return "Pocket " + strings.ToUpper(e.Source[:1]) + e.Source[1:]
}

func timerView(e *pocket.TimeEntry, mine bool) *timerOut {
	if e == nil {
		return nil
	}
	return &timerOut{ID: e.ID, Name: e.Name(), StartedIn: startedIn(e), StartedAt: e.StartedAt, App: e.App, AppRef: e.AppRef, Mine: mine}
}

// timeConn reaches the person's Pocket Time (write: to start and stop).
func (s *Server) timeConn(ctx context.Context, c *clients, write bool) (*siblingConn, error) {
	if !write {
		if sib, err := c.conn(ctx, appTime, scopeTimeRead); err == nil {
			return sib, nil
		}
	}
	return c.conn(ctx, appTime, scopeTimeWrite)
}

// deckRef is what a run is for, in Pocket Time's terms.
func deckRef(d *store.Deck) (ref, label, url string) {
	if d == nil {
		return "deck:all", "All decks", "/study"
	}
	return fmt.Sprintf("deck:%d", d.ID), d.Name, fmt.Sprintf("/decks/%d", d.ID)
}

func sameDeck(x *store.StudySession, d *store.Deck) bool {
	if d == nil {
		return x.DeckID == nil
	}
	return x.DeckID != nil && *x.DeckID == d.ID
}

// quiet reports whether an open session went too long without an answer,
// and when it then ended: at its last answer.
func (s *Server) quiet(x *store.StudySession) (time.Time, bool) {
	if last, ok := store.ParseStamp(x.LastAt); ok && s.now().Sub(last) > sessionIdle {
		return last, true
	}
	return time.Time{}, false
}

// quiet0 is quiet for a session that may be missing or closed.
func (s *Server) quiet0(x *store.StudySession) (time.Time, bool) {
	if x == nil || !x.Open() {
		return time.Time{}, false
	}
	return s.quiet(x)
}

// closeAt is when an open session ends if closed now.
func (s *Server) closeAt(x *store.StudySession) time.Time {
	if at, q := s.quiet(x); q {
		return at
	}
	return s.now()
}

// closeSession ends an open session at a moment. A run Pocket Time times is
// stopped there too (only that timer); Learn's own run without any answer is
// not kept. A failed stop in Time is returned after the row is closed (Time
// still writes back whatever it records, into the same row).
func (s *Server) closeSession(ctx context.Context, c *clients, x *store.StudySession, at time.Time) error {
	if !x.Open() {
		return nil
	}
	var stopErr error
	if x.TimerEntryID > 0 {
		if sib, err := s.timeConn(ctx, c, true); err == nil {
			ended := ""
			if s.now().Sub(at) > time.Minute {
				ended = store.Stamp(at)
			}
			_, err = sib.client.StopTimer(ctx, x.TimerEntryID, ended)
			var ce *pocket.ConflictError
			if err != nil && !errors.As(err, &ce) {
				stopErr = err // another timer running = ours was stopped already
			}
		} else if !isNotConnected(err) {
			stopErr = err
		}
	}
	start, _ := store.ParseStamp(x.StartedAt)
	if at.Before(start) {
		at = start
	}
	x.EndedAt, x.DurationS = store.Stamp(at), int(at.Sub(start).Seconds())
	if x.TimerEntryID == 0 && x.Cards == 0 {
		_ = s.store.DeleteStudySession(ctx, x.UserID, x.ID)
		x.ID = 0
		return stopErr
	}
	if err := s.store.SaveStudySession(ctx, x.UserID, x); err != nil {
		return err
	}
	return stopErr
}

// closeOthers ends the person's open sessions except keep.
func (s *Server) closeOthers(ctx context.Context, c *clients, uid, keep int64) {
	open, _ := s.store.OpenStudySessions(ctx, uid)
	for i := range open {
		if open[i].ID != keep {
			_ = s.closeSession(ctx, c, &open[i], s.closeAt(&open[i]))
		}
	}
}

// newSession records the start of a run (timer = Pocket Time's, or nil).
func (s *Server) newSession(ctx context.Context, uid int64, d *store.Deck, timer *pocket.TimeEntry) (*store.StudySession, error) {
	start := s.now()
	x := &store.StudySession{Source: "app"}
	if timer != nil {
		x.TimerEntryID = timer.ID
		if t, ok := store.ParseStamp(timer.StartedAt); ok && t.Before(start) {
			start = t
		}
	}
	x.StartedAt, x.LastAt, x.Date = store.Stamp(start), store.Stamp(start), store.LocalDate(start)
	if d != nil {
		x.DeckID, x.DeckName = &d.ID, d.Name
	}
	if err := s.store.CreateStudySession(ctx, uid, x); err != nil {
		return nil, err
	}
	return x, nil
}

type startResult struct {
	Session  *sessionOut `json:"session"`            // nil when a timer elsewhere is in the way
	Conflict *timerOut   `json:"conflict,omitempty"` // that timer: offer "Stop it and start this"
	Time     bool        `json:"time"`               // the run is timed in Pocket Time
	Detail   string      `json:"detail,omitempty"`   // why Learn keeps the time itself this once
}

// handleStudyStart starts (or picks up) the person's study run for a deck
// (0 = all decks). switch=true stops a timer running in Pocket Time first.
func (s *Server) handleStudyStart(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in struct {
		Deck   int64 `json:"deck"`
		Switch bool  `json:"switch"`
	}
	_ = decode(r, &in)
	var deck *store.Deck
	if in.Deck > 0 {
		d, err := s.store.GetDeck(ctx, uid, in.Deck)
		if err != nil {
			s.fail(w, err, "deck")
			return
		}
		deck = d
	}
	res, err := s.startRun(ctx, uid, deck, in.Switch)
	if err != nil {
		s.fail(w, err, "start session")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) startRun(ctx context.Context, uid int64, deck *store.Deck, switchTimer bool) (*startResult, error) {
	c := s.clients(ctx, uid)
	open, err := s.store.OpenStudySessions(ctx, uid)
	if err != nil {
		return nil, err
	}
	// the same deck's run still going (a reload, a second tab): pick it up
	var cur *store.StudySession
	for i := range open {
		if _, q := s.quiet(&open[i]); sameDeck(&open[i], deck) && !q {
			cur = &open[i]
		}
	}
	sib, connErr := s.timeConn(ctx, c, true)
	if connErr != nil {
		if cur != nil && cur.TimerEntryID == 0 {
			s.closeOthers(ctx, c, uid, cur.ID)
			return &startResult{Session: s.sessionView(cur)}, nil
		}
		s.closeOthers(ctx, c, uid, 0)
		x, err := s.newSession(ctx, uid, deck, nil)
		if err != nil {
			return nil, err
		}
		res := &startResult{Session: s.sessionView(x)}
		if !isNotConnected(connErr) {
			res.Detail = "Pocket Time did not answer, so Learn keeps this session's time itself: " + connErr.Error()
		}
		return res, nil
	}
	ref, label, url := deckRef(deck)
	ours := map[int64]bool{}
	for _, x := range open {
		if x.TimerEntryID > 0 {
			ours[x.TimerEntryID] = true
		}
	}
	start := pocket.TimerStart{App: "learn", AppRef: ref, AppLabel: label, AppURL: url, Deliver: true, Switch: switchTimer}
	running, err := sib.client.StartTimer(ctx, start)
	var ce *pocket.ConflictError
	if errors.As(err, &ce) && ce.Running != nil {
		switch {
		case ce.Running.App == "learn" && ce.Running.AppRef == ref:
			// this deck is being timed already (this run, or started in Pocket Time for it)
			running, err = ce.Running, nil
		case ours[ce.Running.ID]:
			// Learn's own run of another deck: move the timer on to this one
			start.Switch = true
			running, err = sib.client.StartTimer(ctx, start)
		default:
			return &startResult{Conflict: timerView(ce.Running, false), Time: true}, nil
		}
	}
	if err != nil {
		// Time is unreachable: keep the time here this once
		s.closeOthers(ctx, c, uid, 0)
		x, err2 := s.newSession(ctx, uid, deck, nil)
		if err2 != nil {
			return nil, err2
		}
		return &startResult{Session: s.sessionView(x), Detail: "Pocket Time did not answer, so Learn keeps this session's time itself: " + err.Error()}, nil
	}
	if x, err := s.store.SessionByTimer(ctx, uid, running.ID); err == nil && x.Open() {
		s.closeOthers(ctx, c, uid, x.ID)
		return &startResult{Session: s.sessionView(x), Time: true}, nil
	}
	s.closeOthers(ctx, c, uid, 0)
	x, err := s.newSession(ctx, uid, deck, running)
	if err != nil {
		return nil, err
	}
	return &startResult{Session: s.sessionView(x), Time: true}, nil
}

// endRun closes one of the person's sessions now (Stop, finish, leaving).
func (s *Server) endRun(ctx context.Context, uid, id int64) (*store.StudySession, string) {
	if id <= 0 {
		return nil, ""
	}
	x, err := s.store.GetStudySession(ctx, uid, id)
	if err != nil || !x.Open() {
		return x, ""
	}
	if err := s.closeSession(ctx, s.clients(ctx, uid), x, s.closeAt(x)); err != nil {
		return x, "Pocket Time did not answer, so its timer may still run: " + err.Error()
	}
	return x, ""
}

// handleStudyStop ends a run: the Stop button, or leaving the study screen.
func (s *Server) handleStudyStop(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in struct {
		SessionID int64 `json:"session_id"`
	}
	_ = decode(r, &in)
	x, detail := s.endRun(ctx, actorID(ctx), in.SessionID)
	if x == nil && in.SessionID > 0 {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": s.sessionView(x), "detail": detail})
}

// touchSession counts an answer in its run; a run quiet for too long ends at
// its last answer instead.
func (s *Server) touchSession(ctx context.Context, uid, id int64, rating int) *store.StudySession {
	if id <= 0 {
		return nil
	}
	x, err := s.store.GetStudySession(ctx, uid, id)
	if err != nil || !x.Open() {
		return x
	}
	if at, q := s.quiet(x); q {
		_ = s.closeSession(ctx, s.clients(ctx, uid), x, at)
		return x
	}
	x.Cards++
	if rating == 1 {
		x.Again++
	}
	x.LastAt = store.Stamp(s.now())
	_ = s.store.SaveStudySession(ctx, uid, x)
	return x
}

// untouchSession takes an undone answer out of its run.
func (s *Server) untouchSession(ctx context.Context, uid, id int64) {
	if id <= 0 {
		return
	}
	if x, err := s.store.GetStudySession(ctx, uid, id); err == nil && x.Open() && x.Cards > 0 {
		x.Cards--
		_ = s.store.SaveStudySession(ctx, uid, x)
	}
}

// closeIdleSessions ends the runs nobody answered in for too long (every
// minute), at their last answer. A run whose Pocket Time timer could not be
// stopped is tried again, for a day.
func (s *Server) closeIdleSessions(ctx context.Context) {
	open, err := s.store.OpenStudySessions(ctx, 0)
	if err != nil {
		return
	}
	for i := range open {
		x := &open[i]
		at, q := s.quiet(x)
		if !q {
			continue
		}
		err := s.closeSession(ctx, s.clients(ctx, x.UserID), x, at)
		if err != nil && x.ID > 0 && s.now().Sub(at) < 24*time.Hour {
			x.EndedAt = "" // keep it open: stop it in Time next minute
			_ = s.store.SaveStudySession(ctx, x.UserID, x)
		}
	}
}

// handleTimeTimer is Pocket Time's running timer for the person (polled
// while Learn is open), and, with session=<id>, that run's state: a run
// whose timer was stopped in Pocket Time ends here too.
func (s *Server) handleTimeTimer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	c := s.clients(ctx, uid)
	out := map[string]any{"connected": false, "running": nil}
	var sess *store.StudySession
	if id := int64(queryInt(r, "session")); id > 0 {
		x, err := s.store.GetStudySession(ctx, uid, id)
		if err != nil {
			s.fail(w, err, "session")
			return
		}
		sess = x
	}
	sib, err := s.timeConn(ctx, c, false)
	if err != nil {
		out["detail"] = err.Error()
		if at, q := s.quiet0(sess); q {
			_ = s.closeSession(ctx, c, sess, at)
		}
		out["session"] = s.sessionView(sess)
		writeJSON(w, http.StatusOK, out)
		return
	}
	out["connected"] = true
	if _, _, _, public := connSettings(c.st, appTime); public != "" {
		out["open_url"] = public
	} else {
		out["open_url"] = sib.public
	}
	running, err := sib.client.Timer(ctx)
	if err != nil {
		out["detail"] = "Pocket Time did not answer: " + err.Error()
		out["session"] = s.sessionView(sess)
		writeJSON(w, http.StatusOK, out)
		return
	}
	mine := false
	if running != nil {
		if x, err := s.store.SessionByTimer(ctx, uid, running.ID); err == nil && x.Open() {
			mine = true
		}
	}
	if sess != nil && sess.Open() && sess.TimerEntryID > 0 && (running == nil || running.ID != sess.TimerEntryID) {
		// stopped (or switched) in Pocket Time: this run is over
		start, _ := store.ParseStamp(sess.StartedAt)
		at := s.now()
		sess.EndedAt, sess.DurationS = store.Stamp(at), int(at.Sub(start).Seconds())
		_ = s.store.SaveStudySession(ctx, uid, sess)
	} else if at, q := s.quiet0(sess); q {
		_ = s.closeSession(ctx, c, sess, at)
	}
	out["running"] = timerView(running, mine)
	out["session"] = s.sessionView(sess)
	writeJSON(w, http.StatusOK, out)
}

// handleTimeStop stops the person's timer in Pocket Time (the "Stop" next to
// a timer running there), only if it is still that one.
func (s *Server) handleTimeStop(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in struct {
		ID int64 `json:"id"`
	}
	if err := decode(r, &in); err != nil || in.ID <= 0 {
		writeErr(w, http.StatusBadRequest, "id is the timer to stop")
		return
	}
	c := s.clients(ctx, uid)
	sib, err := s.timeConn(ctx, c, true)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := sib.client.StopTimer(ctx, in.ID, ""); err != nil {
		var ce *pocket.ConflictError
		if errors.As(err, &ce) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "a different timer is running now: " + ce.Running.Name(), "running": timerView(ce.Running, false)})
			return
		}
		writeErr(w, http.StatusBadGateway, "Pocket Time: "+err.Error())
		return
	}
	if x, err := s.store.SessionByTimer(ctx, uid, in.ID); err == nil && x.Open() {
		start, _ := store.ParseStamp(x.StartedAt)
		at := s.now()
		x.EndedAt, x.DurationS = store.Stamp(at), int(at.Sub(start).Seconds())
		_ = s.store.SaveStudySession(ctx, uid, x)
	}
	writeJSON(w, http.StatusOK, map[string]any{"running": nil})
}

// sessionsList is the newest sessions in a period (at most limit) for Today
// and Statistics, with how many there are and their time in seconds.
func (s *Server) sessionsList(ctx context.Context, uid int64, from, to string, deck int64, limit int) ([]sessionOut, int, int) {
	list, _ := s.store.ListStudySessions(ctx, uid, store.SessionFilter{From: from, To: to, Deck: deck})
	out := []sessionOut{}
	total := 0
	for i := range list {
		total += list[i].DurationS
		if len(out) < limit {
			out = append(out, *s.sessionView(&list[i]))
		}
	}
	return out, len(list), total
}

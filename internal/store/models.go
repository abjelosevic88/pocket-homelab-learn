package store

import (
	"strings"
)

// User is an account on this box.
type User struct {
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"` // admin | member
	CreatedAt    string `json:"created_at"`
}

// APIToken is a personal access token (hash stored, plaintext shown once).
type APIToken struct {
	ID         int64   `json:"id"`
	UserID     int64   `json:"user_id"`
	Name       string  `json:"name"`
	Prefix     string  `json:"prefix"`
	LastUsedAt *string `json:"last_used_at"`
	CreatedAt  string  `json:"created_at"`
}

// Card states (the FSRS ones).
const (
	StateNew        = 0
	StateLearning   = 1
	StateReview     = 2
	StateRelearning = 3
)

// Note kinds.
const (
	KindBasic   = "basic"
	KindReverse = "reverse"
	KindCloze   = "cloze"
	KindType    = "type"
)

// ValidKind reports whether k is a note kind.
func ValidKind(k string) bool {
	return k == KindBasic || k == KindReverse || k == KindCloze || k == KindType
}

// Deck is a set of cards studied together.
type Deck struct {
	ID            int64       `json:"id"`
	UserID        int64       `json:"-"`
	Name          string      `json:"name"`
	Description   string      `json:"description"`
	Color         string      `json:"color"`
	NewPerDay     int         `json:"new_per_day"`
	ReviewsPerDay int         `json:"reviews_per_day"`
	Retention     float64     `json:"retention"` // 0 = the person's default
	NewOrder      string      `json:"new_order"` // added | random
	Archived      bool        `json:"archived"`
	Sort          int         `json:"sort"`
	Source        string      `json:"source"`
	ExternalID    string      `json:"external_id"`
	CreatedAt     string      `json:"created_at"`
	UpdatedAt     string      `json:"updated_at"`
	Counts        *DeckCounts `json:"counts,omitempty"`
}

// DeckCounts are a deck's numbers for today.
type DeckCounts struct {
	Total      int `json:"total"`
	New        int `json:"new"`        // never studied
	Learning   int `json:"learning"`   // in a learning or relearning step
	Young      int `json:"young"`      // in review, interval under 21 days
	Mature     int `json:"mature"`     // in review, 21 days or more
	Suspended  int `json:"suspended"`  // set aside
	DueNew     int `json:"due_new"`    // new cards still to learn today (limit applied)
	DueLearn   int `json:"due_learn"`  // learning cards due now
	DueReview  int `json:"due_review"` // reviews due today (limit applied)
	DoneToday  int `json:"done_today"` // answers today
	NewToday   int `json:"new_today"`  // new cards seen today
	DueTomorow int `json:"due_tomorrow"`
}

// Due is everything to study now.
func (c DeckCounts) Due() int { return c.DueNew + c.DueLearn + c.DueReview }

// Note is what a person writes: a question and an answer (or a cloze text).
type Note struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"-"`
	DeckID     int64  `json:"deck_id"`
	Kind       string `json:"kind"`
	Front      string `json:"front"`
	Back       string `json:"back"`
	Extra      string `json:"extra"`
	Tags       string `json:"tags"`
	Source     string `json:"source"`
	SourceRef  string `json:"source_ref"`
	SourceURL  string `json:"source_url"`
	ExternalID string `json:"external_id"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
	DeckName   string `json:"deck_name,omitempty"`
	Cards      []Card `json:"cards,omitempty"`
}

// Card is one thing to remember, with its memory state.
type Card struct {
	ID          int64   `json:"id"`
	UserID      int64   `json:"-"`
	NoteID      int64   `json:"note_id"`
	DeckID      int64   `json:"deck_id"`
	Ord         int     `json:"ord"`
	State       int     `json:"state"`
	Step        int     `json:"step"`
	Stability   float64 `json:"stability"`
	Difficulty  float64 `json:"difficulty"`
	Due         string  `json:"due"`
	LastReview  string  `json:"last_review"`
	Interval    int     `json:"interval"`
	Reps        int     `json:"reps"`
	Lapses      int     `json:"lapses"`
	Position    int     `json:"position"`
	Suspended   bool    `json:"suspended"`
	BuriedUntil string  `json:"buried_until"`
	Flag        int     `json:"flag"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

// Review is one answer.
type Review struct {
	ID             int64   `json:"id"`
	UserID         int64   `json:"-"`
	CardID         int64   `json:"card_id"`
	DeckID         int64   `json:"deck_id"`
	Rating         int     `json:"rating"`
	State          int     `json:"state"`
	ElapsedDays    int     `json:"elapsed_days"`
	LastInterval   int     `json:"last_interval"`
	Interval       int     `json:"interval"`
	Stability      float64 `json:"stability"`
	Difficulty     float64 `json:"difficulty"`
	Retrievability float64 `json:"retrievability"`
	DurationMS     int     `json:"duration_ms"`
	ReviewedAt     string  `json:"reviewed_at"`
	Day            string  `json:"day"`
	Prev           string  `json:"-"`
}

// Media is a picture or a sound used on cards.
type Media struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"-"`
	Name      string `json:"name"`
	Hash      string `json:"hash"`
	Mime      string `json:"mime"`
	Size      int64  `json:"size"`
	CreatedAt string `json:"created_at"`
}

// Settings are the box-wide values plus one person's preferences and their
// own connections. Connection modes: "core" (an app link through Pocket
// Core), "direct" (URL + token) or "off"; "" = not chosen yet.
type Settings struct {
	Title         string `json:"title"`
	SetupComplete bool   `json:"setup_complete"`

	DateFormat string `json:"date_format"`
	TimeFormat string `json:"time_format"`
	WeekStart  int    `json:"week_start"` // 1 = Monday, 7 = Sunday

	// Studying.
	DayStartHour    int     `json:"day_start_hour"`   // a new study day begins at this hour
	Retention       float64 `json:"retention"`        // how sure you want to be to remember (0.7–0.99)
	LearningSteps   string  `json:"learning_steps"`   // "1m 10m"
	RelearningSteps string  `json:"relearning_steps"` // "10m"
	MaxInterval     int     `json:"max_interval"`     // days
	NewPerDay       int     `json:"new_per_day"`      // for new decks
	ReviewsPerDay   int     `json:"reviews_per_day"`
	NewMix          string  `json:"new_mix"` // mix | first | last: new cards among the reviews
	DailyGoal       int     `json:"daily_goal"`
	ShowIntervals   bool    `json:"show_intervals"` // the next interval on each answer button
	ShowTimer       bool    `json:"show_timer"`
	Autoplay        bool    `json:"autoplay"` // play a card's sounds when it is shown
	Speak           bool    `json:"speak"`    // read the question aloud
	SpeakLang       string  `json:"speak_lang"`

	// Reminders.
	ReminderTime     string `json:"reminder_time"` // "" = none, "19:00"
	ReminderDays     string `json:"reminder_days"` // ISO weekdays "1234567"
	ReminderMin      int    `json:"reminder_min"`  // only when at least this many cards are due
	NotifyStreak     bool   `json:"notify_streak"`
	StreakHour       int    `json:"streak_hour"`
	NotifyWeekly     bool   `json:"notify_weekly"`
	WeeklyDay        int    `json:"weekly_day"`
	WeeklyHour       int    `json:"weekly_hour"`
	NotifyURL        string `json:"notify_url"`
	NotifyFormat     string `json:"notify_format"` // apprise | ntfy | json
	NotifyMilestones bool   `json:"notify_milestones"`

	ReaderMode      string `json:"reader_mode"`
	ReaderURL       string `json:"reader_url"`
	ReaderToken     string `json:"reader_token"`
	ReaderPublicURL string `json:"reader_public_url"`

	LibraryMode      string `json:"library_mode"`
	LibraryURL       string `json:"library_url"`
	LibraryToken     string `json:"library_token"`
	LibraryPublicURL string `json:"library_public_url"`

	NotesMode      string `json:"notes_mode"`
	NotesURL       string `json:"notes_url"`
	NotesToken     string `json:"notes_token"`
	NotesPublicURL string `json:"notes_public_url"`

	// Pocket Time: the study timer is the person's one timer there.
	TimeMode      string `json:"time_mode"`
	TimeURL       string `json:"time_url"`
	TimeToken     string `json:"time_token"`
	TimePublicURL string `json:"time_public_url"`

	// AI for making cards and explaining them: Pocket AI through Core, or an
	// OpenAI-compatible API with the person's own key.
	AIProvider    string `json:"ai_provider"` // auto | pocket | openai
	AIPocketModel string `json:"ai_pocket_model"`
	OpenAIKey     string `json:"openai_api_key"`
	OpenAIModel   string `json:"openai_model"`
	OpenAIBaseURL string `json:"openai_base_url"`
}

// SplitTags turns "a, b ,c" or "a b c" into clean lowercase tags.
func SplitTags(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' }) {
		t = strings.ToLower(strings.Trim(strings.TrimSpace(t), "#"))
		if t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// JoinTags is the stored form of tags: " a b c " (spaces around, so a LIKE '% a %' finds one).
func JoinTags(s string) string {
	t := SplitTags(s)
	if len(t) == 0 {
		return ""
	}
	return strings.Join(t, " ")
}

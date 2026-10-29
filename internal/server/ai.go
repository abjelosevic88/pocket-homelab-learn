package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-learn/internal/ai"
	"github.com/abjelosevic88/pocket-homelab-learn/internal/store"
)

// aiFor is the person's AI: Pocket AI through Core, their own
// OpenAI-compatible key, or nil with why.
func (s *Server) aiFor(ctx context.Context, uid int64, st store.Settings) (*ai.Client, string, string) {
	email := ""
	if u, err := s.store.GetUser(ctx, uid); err == nil {
		email = u.Email
	}
	pocket, status := s.core.SelectAI(ctx, email, st.AIProvider, st.OpenAIKey != "")
	if pocket != nil {
		if !status.Ready {
			return nil, "", "Pocket AI: " + status.Detail
		}
		c := ai.New("", "", "", 150*time.Second)
		c.Pocket = pocket.WithModel(st.AIPocketModel)
		return c, "pocket", ""
	}
	if st.OpenAIKey == "" {
		return nil, "", status.Detail
	}
	return ai.New(st.OpenAIBaseURL, st.OpenAIKey, st.OpenAIModel, 150*time.Second), "openai", ""
}

func (s *Server) handleAIStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st, _ := s.store.GetSettings(ctx, actorID(ctx))
	cl, used, why := s.aiFor(ctx, actorID(ctx), st)
	writeJSON(w, http.StatusOK, map[string]any{"ready": cl != nil, "provider": used, "detail": why})
}

func (s *Server) handleAIModels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c, status := s.core.SelectAI(ctx, userFrom(ctx).Email, "pocket", false)
	if c == nil || !status.Ready {
		writeErr(w, http.StatusServiceUnavailable, status.Detail)
		return
	}
	models, err := c.Models(ctx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, models)
}

func validAIProvider(v string) bool { return core.ValidAIProvider(v) }

func validPocketModel(v string) bool { return core.ValidInferenceModel(v) }

const cardVoice = `You write flashcards for spaced repetition, the way a good teacher would. Rules:
- One idea per card. The question must make sense on its own, without the source in front of you.
- Short, exact answers: a word, a name, a number, a short phrase. No yes/no questions, no "true or false".
- Prefer the facts, terms, definitions, causes and numbers that matter; skip trivia and examples that only illustrate.
- Write in the language of the text unless asked otherwise. Keep names and terms as the text has them.
- For a cloze card the front is one sentence from the text with the key part as {{c1::part}} (a second gap in the same sentence is {{c2::…}}); its back is empty.
- Never invent facts that are not in the text.
Answer with JSON only: {"cards":[{"kind":"basic"|"cloze","front":"…","back":"…","extra":"a short hint or context, may be empty"}]}`

type aiCard struct {
	Kind  string `json:"kind"`
	Front string `json:"front"`
	Back  string `json:"back"`
	Extra string `json:"extra"`
}

// parseCards reads the model's JSON (tolerating code fences and a bare array).
func parseCards(text string) ([]aiCard, error) {
	t := strings.TrimSpace(text)
	t = strings.TrimPrefix(t, "```json")
	t = strings.TrimPrefix(t, "```")
	t = strings.TrimSuffix(t, "```")
	var doc struct {
		Cards []aiCard `json:"cards"`
	}
	if i, j := strings.Index(t, "{"), strings.LastIndex(t, "}"); i >= 0 && j > i {
		if err := json.Unmarshal([]byte(t[i:j+1]), &doc); err == nil && len(doc.Cards) > 0 {
			return doc.Cards, nil
		}
	}
	if i, j := strings.Index(t, "["), strings.LastIndex(t, "]"); i >= 0 && j > i {
		if err := json.Unmarshal([]byte(t[i:j+1]), &doc.Cards); err == nil {
			return doc.Cards, nil
		}
	}
	return nil, fmt.Errorf("the AI did not answer with cards; try again")
}

// handleAICards writes flashcards from a text (pasted, a note, a highlight).
// Nothing is saved: the person picks the cards they want.
func (s *Server) handleAICards(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	st, _ := s.store.GetSettings(ctx, uid)
	cl, used, why := s.aiFor(ctx, uid, st)
	if cl == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": why})
		return
	}
	var in struct {
		Text     string `json:"text"`
		Title    string `json:"title"`
		Count    int    `json:"count"`
		Kind     string `json:"kind"` // auto | basic | cloze
		Language string `json:"language"`
		Focus    string `json:"focus"`
	}
	if err := decode(r, &in); err != nil || strings.TrimSpace(in.Text) == "" {
		writeErr(w, http.StatusBadRequest, "give the AI a text to make cards from")
		return
	}
	text := strings.TrimSpace(in.Text)
	if r := []rune(text); len(r) > 40000 {
		text = string(r[:40000])
	}
	if in.Count <= 0 {
		in.Count = 8
	}
	in.Count = min(in.Count, 40)
	ask := fmt.Sprintf("Write up to %d flashcards", in.Count)
	switch in.Kind {
	case "basic":
		ask += " as question and answer (kind basic only)"
	case "cloze":
		ask += " as cloze sentences (kind cloze only)"
	default:
		ask += "; use question and answer for facts and definitions, cloze for key sentences"
	}
	if l := strings.TrimSpace(in.Language); l != "" {
		ask += ", written in " + l
	}
	if f := strings.TrimSpace(in.Focus); f != "" {
		ask += ". Focus on: " + f
	}
	ask += ". Fewer, better cards are fine when the text has less to learn."
	if t := strings.TrimSpace(in.Title); t != "" {
		ask += "\n\nThe text is from: " + t
	}
	out, err := cl.Complete(ctx, cardVoice, ask+"\n\nText:\n"+text, true)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	cards, err := parseCards(out)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	good := []aiCard{}
	for _, c := range cards {
		n := store.Note{Kind: c.Kind, Front: c.Front, Back: c.Back, Extra: c.Extra}
		if n.Kind != store.KindCloze {
			n.Kind = store.KindBasic
		}
		if checkNote(&n) != "" {
			continue
		}
		good = append(good, aiCard{Kind: n.Kind, Front: n.Front, Back: n.Back, Extra: n.Extra})
	}
	if len(good) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "the AI found nothing to make cards from"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "provider": used, "cards": good})
}

const tutorVoice = `You are a patient, plain-spoken tutor helping someone remember a flashcard. Be short and concrete. No emojis, no exclamation marks, no filler. Plain text; no headings.`

// handleExplain explains a card and gives a way to remember it.
func (s *Server) handleExplain(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	st, _ := s.store.GetSettings(ctx, uid)
	cl, used, why := s.aiFor(ctx, uid, st)
	if cl == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": why})
		return
	}
	var in struct {
		CardID int64 `json:"card_id"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "card_id is required")
		return
	}
	b, err := s.store.Brief(ctx, uid, in.CardID)
	if err != nil {
		s.fail(w, err, "card")
		return
	}
	q, a := cardSides(b)
	prompt := fmt.Sprintf("Deck: %s\nQuestion: %s\nAnswer: %s\n", b.Deck, q, a)
	if b.Extra != "" {
		prompt += "More: " + b.Extra + "\n"
	}
	if b.Lapses > 0 {
		prompt += fmt.Sprintf("They forgot this card %d times.\n", b.Lapses)
	}
	prompt += "\nIn 3–5 sentences explain why the answer is what it is, then give one memory aid (a mnemonic, a vivid picture or a link to something well known), starting that line with 'To remember:'."
	text, err := cl.Complete(ctx, tutorVoice, prompt, false)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "provider": used, "text": strings.TrimSpace(text)})
}

// cardSides is a card's question and answer as plain text (a cloze gap shown as […]).
func cardSides(b *store.CardBrief) (string, string) {
	switch b.Kind {
	case store.KindCloze:
		return clozeText(b.Front, b.Ord, false), clozeText(b.Front, b.Ord, true)
	case store.KindReverse:
		if b.Ord == 1 {
			return b.Back, b.Front
		}
	}
	return b.Front, b.Back
}

var clozeAll = regexp.MustCompile(`\{\{c(\d+)::(.*?)(?:::(.*?))?\}\}`)

// clozeText renders a cloze note for card n: its gap hidden ([…] or the hint) or shown.
func clozeText(text string, n int, reveal bool) string {
	return clozeAll.ReplaceAllStringFunc(text, func(m string) string {
		p := clozeAll.FindStringSubmatch(m)
		num, _ := strconv.Atoi(p[1])
		if num != n || reveal {
			return p[2]
		}
		if p[3] != "" {
			return "[" + p[3] + "]"
		}
		return "[…]"
	})
}

// studyDigest renders the person's study as compact text for the model.
func (s *Server) studyDigest(ctx context.Context, uid int64) string {
	st, _ := s.store.GetSettings(ctx, uid)
	var b strings.Builder
	decks, ck, err := s.decksWithCounts(ctx, uid, st, false)
	if err != nil {
		return ""
	}
	cur, best := s.streaks(ctx, uid, st)
	life, _ := s.store.LifetimeTotals(ctx, uid)
	fmt.Fprintf(&b, "Today %s. Streak %d days (longest %d). Lifetime %d answers on %d days, %d notes. Desired retention %.0f%%.\n", ck.today(), cur, best, life.Reviews, life.Days, life.Notes, st.Retention*100)
	b.WriteString("Decks (name | cards | new | learning | young | mature | due today):\n")
	for _, d := range decks {
		c := d.Counts
		fmt.Fprintf(&b, "%s | %d | %d | %d | %d | %d | %d\n", d.Name, c.Total, c.New, c.Learning, c.Young, c.Mature, c.Due())
	}
	from := store.AddDays(ck.today(), -29)
	days, _ := s.store.DayStats(ctx, uid, from, ck.today(), 0)
	b.WriteString("Last 30 days (day | answers | new | again on reviews | minutes):\n")
	for _, d := range days {
		fmt.Fprintf(&b, "%s | %d | %d | %d of %d | %.0f\n", d.Day, d.Reviews, d.New, d.Again, d.Review, float64(d.DurationMS)/60000)
	}
	if hard, err := s.store.HardCards(ctx, uid, 3, 12); err == nil && len(hard) > 0 {
		b.WriteString("Cards forgotten most (question → answer, times forgotten):\n")
		for _, h := range hard {
			q, a := cardSides(&h)
			fmt.Fprintf(&b, "%s → %s (%d)\n", clip(q, 120), clip(a, 80), h.Lapses)
		}
	}
	return b.String()
}

func clip(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

const coachVoice = `You are a calm, practical study coach looking at someone's own flashcard statistics. Use only the data given; never invent numbers. Be concrete and short. No emojis, no exclamation marks, no motivational filler.`

// handleAsk answers a question about the person's studying.
func (s *Server) handleAsk(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	st, _ := s.store.GetSettings(ctx, uid)
	cl, used, why := s.aiFor(ctx, uid, st)
	if cl == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": why})
		return
	}
	var in struct {
		Question string `json:"question"`
	}
	if err := decode(r, &in); err != nil || strings.TrimSpace(in.Question) == "" {
		writeErr(w, http.StatusBadRequest, "question is required")
		return
	}
	prompt := "Question: " + strings.TrimSpace(in.Question) + "\n\nTheir study:\n" + s.studyDigest(ctx, uid) +
		"\nAnswer in a few sentences, with the data that backs it. If the data cannot answer it, say so and answer from general knowledge about spaced repetition, marked as such."
	text, err := cl.Complete(ctx, coachVoice, prompt, false)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "provider": used, "text": strings.TrimSpace(text)})
}

package srs

import (
	"math"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

func TestNewCardGoodEntersSecondStep(t *testing.T) {
	p := DefaultParams()
	o := p.Review(Card{}, Good, t0, 0, 1)
	if o.Card.State != Learning || o.Card.Step != 1 || o.Wait != 10*time.Minute {
		t.Fatalf("got state %d step %d wait %s", o.Card.State, o.Card.Step, o.Wait)
	}
	if o.Card.Stability != DefaultW[2] || o.Card.Reps != 1 || !o.Card.LastReview.Equal(t0) || !o.Card.Due.Equal(t0.Add(10*time.Minute)) {
		t.Fatalf("card %+v", o.Card)
	}
	// Good again graduates.
	o2 := p.Review(o.Card, Good, t0.Add(10*time.Minute), 0, 1)
	if o2.Card.State != Review || o2.IntervalDays < 1 || o2.Wait != 0 {
		t.Fatalf("graduation: %+v", o2)
	}
}

func TestNewCardSteps(t *testing.T) {
	p := DefaultParams()
	if o := p.Review(Card{}, Again, t0, 0, 1); o.Wait != time.Minute || o.Card.Step != 0 {
		t.Fatalf("again: %+v", o)
	}
	if o := p.Review(Card{}, Hard, t0, 0, 1); o.Wait != 330*time.Second {
		t.Fatalf("hard: %s", o.Wait)
	}
	p.LearningSteps = []time.Duration{10 * time.Minute}
	if o := p.Review(Card{}, Hard, t0, 0, 1); o.Wait != 15*time.Minute {
		t.Fatalf("hard single step: %s", o.Wait)
	}
	p.LearningSteps = nil
	if o := p.Review(Card{}, Again, t0, 0, 1); o.Card.State != Review || o.IntervalDays < 1 {
		t.Fatalf("no steps: %+v", o)
	}
}

func TestNewCardEasy(t *testing.T) {
	p := DefaultParams()
	p.Fuzz = false
	o := p.Review(Card{}, Easy, t0, 0, 1)
	want := p.NextInterval(DefaultW[3])
	if o.Card.State != Review || o.IntervalDays != want || want < 15 || want > 16 {
		t.Fatalf("easy interval %d want %d", o.IntervalDays, want)
	}
	if !o.Card.Due.Equal(t0.Add(time.Duration(want) * 24 * time.Hour)) {
		t.Fatalf("due %s", o.Card.Due)
	}
}

func TestIntervalEqualsStabilityAt90(t *testing.T) {
	p := DefaultParams()
	for _, s := range []float64{1, 3.4, 10, 42.6, 300} {
		if got := p.NextInterval(s); got != int(math.Round(s)) {
			t.Fatalf("S=%v interval %d", s, got)
		}
	}
	p.MaximumInterval = 100
	if p.NextInterval(500) != 100 || p.NextInterval(0.01) != 1 {
		t.Fatal("clamp")
	}
	hi := DefaultParams()
	hi.DesiredRetention = 0.97
	if hi.NextInterval(100) >= p.NextInterval(100) {
		t.Fatal("higher retention should give shorter intervals")
	}
}

func mature() Card {
	return Card{State: Review, Stability: 30, Difficulty: 5, LastReview: t0.AddDate(0, 0, -30), Due: t0, Reps: 6}
}

func TestReviewAgainRelearns(t *testing.T) {
	p := DefaultParams()
	o := p.Review(mature(), Again, t0, 30, 1)
	if o.Card.State != Relearning || o.Card.Lapses != 1 || o.Wait != 10*time.Minute || o.Card.Stability >= 30 {
		t.Fatalf("%+v", o)
	}
	// relearned with Good goes back to review
	o2 := p.Review(o.Card, Good, t0.Add(10*time.Minute), 0, 1)
	if o2.Card.State != Review || o2.IntervalDays < 1 || o2.Card.Lapses != 1 {
		t.Fatalf("%+v", o2)
	}
	p.RelearningSteps = nil
	if o := p.Review(mature(), Again, t0, 30, 1); o.Card.State != Review || o.IntervalDays < 1 || o.Card.Lapses != 1 {
		t.Fatalf("no relearning steps: %+v", o)
	}
}

func TestReviewOrderAndDeterminism(t *testing.T) {
	p := DefaultParams()
	h := p.Review(mature(), Hard, t0, 30, 7)
	g := p.Review(mature(), Good, t0, 30, 7)
	e := p.Review(mature(), Easy, t0, 30, 7)
	if !(h.IntervalDays <= g.IntervalDays && g.IntervalDays <= e.IntervalDays) {
		t.Fatalf("hard %d good %d easy %d", h.IntervalDays, g.IntervalDays, e.IntervalDays)
	}
	if g2 := p.Review(mature(), Good, t0, 30, 7); g2.IntervalDays != g.IntervalDays || !g2.Card.Due.Equal(g.Card.Due) {
		t.Fatal("same seed, different result")
	}
	nf := DefaultParams()
	nf.Fuzz = false
	base := nf.Review(mature(), Good, t0, 30, 7).IntervalDays
	seen := map[int]bool{}
	for seed := int64(0); seed < 50; seed++ {
		ivl := p.Review(mature(), Good, t0, 30, seed).IntervalDays
		if math.Abs(float64(ivl-base)) > float64(base)*0.1+2 {
			t.Fatalf("fuzz too wide: %d vs %d", ivl, base)
		}
		seen[ivl] = true
	}
	if len(seen) < 2 {
		t.Fatal("fuzz never varied")
	}
}

func TestDifficultyBounds(t *testing.T) {
	p := DefaultParams()
	c := mature()
	for i := 0; i < 20; i++ {
		c = p.Review(c, Again, t0, 1, 1).Card
		c.State = Review
	}
	if c.Difficulty > 10 || c.Difficulty < 1 {
		t.Fatalf("difficulty %v", c.Difficulty)
	}
	if r := p.Retrievability(Card{}, 10); r != 1 {
		t.Fatal("new card retrievability")
	}
	if r := p.Retrievability(Card{State: Review, Stability: 10}, 10); math.Abs(r-0.9) > 1e-9 {
		t.Fatalf("R(S,S)=%v", r)
	}
}

func TestSteps(t *testing.T) {
	d, err := ParseSteps("1m, 10m 1h  2d 1.5h")
	if err != nil {
		t.Fatal(err)
	}
	want := []time.Duration{time.Minute, 10 * time.Minute, time.Hour, 48 * time.Hour, 90 * time.Minute}
	if len(d) != len(want) {
		t.Fatalf("%v", d)
	}
	for i := range d {
		if d[i] != want[i] {
			t.Fatalf("%v", d)
		}
	}
	if s := FormatSteps(d); s != "1m 10m 1h 2d 90m" {
		t.Fatalf("format %q", s)
	}
	if d, err := ParseSteps(""); err != nil || len(d) != 0 {
		t.Fatal("empty")
	}
	for _, bad := range []string{"10", "5s", "0m", "31d", "xm", "-1m"} {
		if _, err := ParseSteps(bad); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
	p := DefaultParams()
	if p.Validate() != nil {
		t.Fatal("defaults invalid")
	}
	p.DesiredRetention = 0.5
	if p.Validate() == nil {
		t.Fatal("retention accepted")
	}
}

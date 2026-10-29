// Package srs schedules flashcard reviews with FSRS-5 (the Free Spaced
// Repetition Scheduler Anki uses), following py-fsrs v5: new cards go through
// learning steps, graduate to review, and lapsed cards relearn.
package srs

import (
	"errors"
	"math"
	"math/rand"
	"time"
)

// State is where a card is in its life.
type State int

const (
	New        State = 0
	Learning   State = 1
	Review     State = 2
	Relearning State = 3
)

// Rating is the answer button.
type Rating int

const (
	Again Rating = 1
	Hard  Rating = 2
	Good  Rating = 3
	Easy  Rating = 4
)

// Card is the scheduling state of one card.
type Card struct {
	State      State
	Step       int
	Stability  float64 // 0 = not set yet (a new card)
	Difficulty float64
	Due        time.Time
	LastReview time.Time // zero = never reviewed
	Reps       int
	Lapses     int
}

// Params are the scheduler's settings.
type Params struct {
	W                [19]float64
	DesiredRetention float64 // 0.70–0.99
	LearningSteps    []time.Duration
	RelearningSteps  []time.Duration
	MaximumInterval  int // days
	Fuzz             bool
}

// DefaultW are the FSRS-5 default weights.
var DefaultW = [19]float64{0.40255, 1.18385, 3.173, 15.69105, 7.1949, 0.5345, 1.4604, 0.0046, 1.54575, 0.1192, 1.01925, 1.9395, 0.11, 0.29605, 2.2698, 0.2315, 2.9898, 0.51655, 0.6621}

const (
	decay = -0.5
)

// factor makes R = 0.9 when t = S.
var factor = math.Pow(0.9, 1/decay) - 1

// DefaultParams are FSRS-5 defaults: 90% retention, learning steps 1m 10m,
// relearning 10m, at most 100 years between reviews, fuzz on.
func DefaultParams() Params {
	return Params{
		W:                DefaultW,
		DesiredRetention: 0.9,
		LearningSteps:    []time.Duration{time.Minute, 10 * time.Minute},
		RelearningSteps:  []time.Duration{10 * time.Minute},
		MaximumInterval:  36500,
		Fuzz:             true,
	}
}

// Validate checks the settings a person can change.
func (p Params) Validate() error {
	if p.DesiredRetention < 0.7 || p.DesiredRetention > 0.99 {
		return errors.New("retention is 70% to 99%")
	}
	if p.MaximumInterval < 1 || p.MaximumInterval > 36500 {
		return errors.New("the longest interval is 1 to 36500 days")
	}
	for _, steps := range [][]time.Duration{p.LearningSteps, p.RelearningSteps} {
		if len(steps) > maxSteps {
			return errors.New("at most 30 steps")
		}
		for _, d := range steps {
			if d < minStep || d > maxStep {
				return errors.New("each step is 1 minute to 30 days")
			}
		}
	}
	return nil
}

// Outcome is what answering does to a card.
type Outcome struct {
	Card         Card          // the card after the review
	IntervalDays int           // > 0 when the card is (now) in Review: the days until it is due
	Wait         time.Duration // > 0 when it is in a (re)learning step: the wait
}

func clamp(v, lo, hi float64) float64 { return math.Min(math.Max(v, lo), hi) }

func (p Params) initStability(r Rating) float64 { return math.Max(p.W[r-1], 0.01) }

func (p Params) initDifficulty(r Rating) float64 {
	return clamp(p.W[4]-math.Exp(p.W[5]*float64(r-1))+1, 1, 10)
}

func (p Params) nextDifficulty(d float64, r Rating) float64 {
	delta := -p.W[6] * float64(r-3)
	d2 := d + delta*(10-d)/9
	d3 := p.W[7]*p.initDifficulty(Easy) + (1-p.W[7])*d2
	return clamp(d3, 1, 10)
}

func (p Params) recallStability(d, s, r float64, g Rating) float64 {
	hard, easy := 1.0, 1.0
	if g == Hard {
		hard = p.W[15]
	}
	if g == Easy {
		easy = p.W[16]
	}
	return s * (1 + math.Exp(p.W[8])*(11-d)*math.Pow(s, -p.W[9])*(math.Exp(p.W[10]*(1-r))-1)*hard*easy)
}

func (p Params) forgetStability(d, s, r float64) float64 {
	long := p.W[11] * math.Pow(d, -p.W[12]) * (math.Pow(s+1, p.W[13]) - 1) * math.Exp(p.W[14]*(1-r))
	short := s / math.Exp(p.W[17]*p.W[18])
	return math.Min(long, short)
}

func (p Params) shortTermStability(s float64, g Rating) float64 {
	inc := math.Exp(p.W[17] * (float64(g) - 3 + p.W[18]))
	if g >= Good {
		inc = math.Max(inc, 1)
	}
	return s * inc
}

// Retrievability is the chance of recalling the card after elapsedDays (1 for a new card).
func (p Params) Retrievability(c Card, elapsedDays float64) float64 {
	if c.State == New || c.Stability <= 0 {
		return 1
	}
	if elapsedDays < 0 {
		elapsedDays = 0
	}
	return math.Pow(1+factor*elapsedDays/c.Stability, decay)
}

// NextInterval is the unfuzzed interval in days for a stability, clamped to [1, MaximumInterval].
func (p Params) NextInterval(stability float64) int {
	ret := p.DesiredRetention
	if ret <= 0 || ret >= 1 {
		ret = 0.9
	}
	ivl := stability / factor * (math.Pow(ret, 1/decay) - 1)
	n := int(math.Round(ivl))
	maxI := p.MaximumInterval
	if maxI < 1 {
		maxI = 36500
	}
	if n < 1 {
		n = 1
	}
	if n > maxI {
		n = maxI
	}
	return n
}

var fuzzRanges = []struct{ start, end, factor float64 }{
	{2.5, 7, 0.15},
	{7, 20, 0.1},
	{20, math.Inf(1), 0.05},
}

func (p Params) fuzz(ivl int, seed int64) int {
	if !p.Fuzz || float64(ivl) < 2.5 {
		return ivl
	}
	f := float64(ivl)
	delta := 1.0
	for _, r := range fuzzRanges {
		delta += r.factor * math.Max(math.Min(f, r.end)-r.start, 0)
	}
	maxI := p.MaximumInterval
	if maxI < 1 {
		maxI = 36500
	}
	lo := int(math.Max(2, math.Round(f-delta)))
	hi := int(math.Min(math.Round(f+delta), float64(maxI)))
	if lo > hi {
		lo = hi
	}
	rng := rand.New(rand.NewSource(seed))
	return lo + rng.Intn(hi-lo+1)
}

// Review answers a card. elapsedDays is the whole study days since the last
// review (the caller counts them with its own day boundary; ignored for a new
// card). seed makes the fuzz deterministic, so a preview of the four buttons
// equals what answering gives.
func (p Params) Review(c Card, r Rating, now time.Time, elapsedDays int, seed int64) Outcome {
	if r < Again || r > Easy {
		r = Good
	}
	if elapsedDays < 0 {
		elapsedDays = 0
	}
	first := c.State == New || c.Stability <= 0 || c.LastReview.IsZero()
	if c.State == New {
		c.State = Learning
		c.Step = 0
	}
	out := Outcome{}
	toReview := func() {
		c.State = Review
		c.Step = 0
		ivl := p.fuzz(p.NextInterval(c.Stability), seed)
		if ivl < 1 {
			ivl = 1
		}
		out.IntervalDays = ivl
	}
	wait := func(d time.Duration) { out.Wait = d }

	switch c.State {
	case Learning, Relearning:
		if first {
			c.Stability = p.initStability(r)
			c.Difficulty = p.initDifficulty(r)
		} else {
			if elapsedDays < 1 {
				c.Stability = p.shortTermStability(c.Stability, r)
			} else {
				ret := p.Retrievability(c, float64(elapsedDays))
				if r == Again {
					c.Stability = p.forgetStability(c.Difficulty, c.Stability, ret)
				} else {
					c.Stability = p.recallStability(c.Difficulty, c.Stability, ret, r)
				}
			}
			c.Difficulty = p.nextDifficulty(c.Difficulty, r)
		}
		steps := p.LearningSteps
		if c.State == Relearning {
			steps = p.RelearningSteps
		}
		if len(steps) == 0 || (c.Step >= len(steps) && r != Again) {
			toReview()
			break
		}
		if c.Step >= len(steps) {
			c.Step = 0
		}
		switch r {
		case Again:
			c.Step = 0
			wait(steps[0])
		case Hard:
			switch {
			case c.Step == 0 && len(steps) == 1:
				wait(steps[0] * 3 / 2)
			case c.Step == 0:
				wait((steps[0] + steps[1]) / 2)
			default:
				wait(steps[c.Step])
			}
		case Good:
			if c.Step+1 >= len(steps) {
				toReview()
			} else {
				c.Step++
				wait(steps[c.Step])
			}
		case Easy:
			toReview()
		}
	case Review:
		if elapsedDays < 1 {
			c.Stability = p.shortTermStability(c.Stability, r)
		} else {
			ret := p.Retrievability(c, float64(elapsedDays))
			if r == Again {
				c.Stability = p.forgetStability(c.Difficulty, c.Stability, ret)
			} else {
				c.Stability = p.recallStability(c.Difficulty, c.Stability, ret, r)
			}
		}
		c.Difficulty = p.nextDifficulty(c.Difficulty, r)
		if r == Again {
			c.Lapses++
			if len(p.RelearningSteps) > 0 {
				c.State = Relearning
				c.Step = 0
				wait(p.RelearningSteps[0])
				break
			}
		}
		toReview()
	}
	c.Reps++
	c.LastReview = now
	if out.Wait > 0 {
		c.Due = now.Add(out.Wait)
	} else {
		c.Due = now.Add(time.Duration(out.IntervalDays) * 24 * time.Hour)
	}
	out.Card = c
	return out
}

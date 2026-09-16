package srs

import (
	"fmt"
	"math"
	"time"
)

// Rating represents the user recall feedback.
type Rating int

const (
	RatingAgain Rating = 1 // Forgotten / incorrect
	RatingHard  Rating = 2 // Recalled with significant effort
	RatingGood  Rating = 3 // Recalled with normal effort
	RatingEasy  Rating = 4 // Recalled effortlessly
)

func (r Rating) String() string {
	switch r {
	case RatingAgain:
		return "Again"
	case RatingHard:
		return "Hard"
	case RatingGood:
		return "Good"
	case RatingEasy:
		return "Easy"
	default:
		return "Unknown"
	}
}

// State represents the learning state of a card.
type State int

const (
	StateNew        State = 0
	StateLearning   State = 1
	StateReview     State = 2
	StateRelearning State = 3
)

func (s State) String() string {
	switch s {
	case StateNew:
		return "New"
	case StateLearning:
		return "Learning"
	case StateReview:
		return "Review"
	case StateRelearning:
		return "Relearning"
	default:
		return "Unknown"
	}
}

// CardRecord contains the SRS scheduling fields of a flashcard.
type CardRecord struct {
	ID             string    `json:"id"`
	State          State     `json:"state"`
	Stability      float64   `json:"stability"`
	Difficulty     float64   `json:"difficulty"`
	Reps           int       `json:"reps"`
	Lapses         int       `json:"lapses"`
	LastReviewedAt *time.Time `json:"last_reviewed_at,omitempty"`
	DueAt          time.Time `json:"due_at"`
}

// ReviewResult encapsulates the updated card record and the review log details.
type ReviewResult struct {
	Card          CardRecord    `json:"card"`
	Rating        Rating        `json:"rating"`
	ScheduledDays float64       `json:"scheduled_days"`
	ElapsedDays   float64       `json:"elapsed_days"`
	ReviewedAt    time.Time     `json:"reviewed_at"`
	NextInterval  time.Duration `json:"next_interval"`
}

// FSRS implements the Free Spaced Repetition Scheduler algorithm.
type FSRS struct {
	Parameters       [19]float64
	RequestRetention float64
	MaximumInterval  int
}

// DefaultParameters holds standard FSRS-v4.5/v5 weights.
var DefaultParameters = [19]float64{
	0.40255, 1.18385, 3.173, 15.69105, // w0-w3: initial stability for Again, Hard, Good, Easy
	7.1949, 0.5345, // w4-w5: initial difficulty parameters
	1.4604, 0.0046, // w6-w7: difficulty update and mean reversion
	1.5457, 0.1192, 1.0192, // w8-w10: stability recall updates
	1.9395, 0.11, 0.29605, 0.22695, // w11-w14: stability lapse updates
	0.2315, 2.9898, // w15-w16: hard and easy bonuses/penalties
	0.51655, 0.6621, // w17-w18: short term stability / additional decay
}

const (
	defaultRetention = 0.90
	defaultMaxInterval = 36500 // 100 years
	factor = 19.0 / 81.0
)

// New creates a new FSRS scheduler instance with default parameters.
func New() *FSRS {
	return &FSRS{
		Parameters:       DefaultParameters,
		RequestRetention: defaultRetention,
		MaximumInterval:  defaultMaxInterval,
	}
}

// Retrievability calculates memory retention probability given elapsed days and stability.
func (f *FSRS) Retrievability(elapsedDays, stability float64) float64 {
	if stability <= 0 {
		return 0
	}
	if elapsedDays <= 0 {
		return 1.0
	}
	r := math.Pow(1.0+factor*(elapsedDays/stability), -0.5)
	if r < 0 {
		return 0
	}
	if r > 1 {
		return 1
	}
	return r
}

// InitialDifficulty calculates starting difficulty for a given rating.
func (f *FSRS) InitialDifficulty(rating Rating) float64 {
	g := float64(rating)
	d := f.Parameters[4] - math.Exp(f.Parameters[5]*(g-1.0)) + 1.0
	return clamp(d, 1.0, 10.0)
}

// InitialStability calculates starting stability for a given rating.
func (f *FSRS) InitialStability(rating Rating) float64 {
	idx := int(rating) - 1
	if idx < 0 || idx >= 4 {
		idx = 2 // default to Good
	}
	return math.Max(0.1, f.Parameters[idx])
}

// NextDifficulty calculates updated difficulty after review.
func (f *FSRS) NextDifficulty(d float64, rating Rating) float64 {
	g := float64(rating)
	deltaD := -f.Parameters[6] * (g - 3.0)
	d0 := f.InitialDifficulty(RatingGood)
	nextD := f.Parameters[7]*d0 + (1.0-f.Parameters[7])*(d+deltaD)
	return clamp(nextD, 1.0, 10.0)
}

// NextRecallStability calculates updated stability after successful recall (Hard, Good, Easy).
func (f *FSRS) NextRecallStability(d, s, r float64, rating Rating) float64 {
	hardPenalty := 1.0
	if rating == RatingHard {
		hardPenalty = f.Parameters[15]
	}
	easyBonus := 1.0
	if rating == RatingEasy {
		easyBonus = f.Parameters[16]
	}

	w8 := f.Parameters[8]
	w9 := f.Parameters[9]
	w10 := f.Parameters[10]

	factorVal := 1.0 + math.Exp(w8)*(11.0-d)*math.Pow(s, -w9)*(math.Exp(w10*(1.0-r))-1.0)*hardPenalty*easyBonus
	return math.Max(0.1, s*factorVal)
}

// NextForgetStability calculates updated stability after forgetting (Again).
func (f *FSRS) NextForgetStability(d, s, r float64) float64 {
	w11 := f.Parameters[11]
	w12 := f.Parameters[12]
	w13 := f.Parameters[13]
	w14 := f.Parameters[14]

	newS := w11 * math.Pow(d, -w12) * (math.Pow(s+1.0, w13) - 1.0) * math.Exp(w14*(1.0-r))
	// Stability should not increase after a lapse
	if newS > s && s > 0 {
		newS = s
	}
	return math.Max(0.1, newS)
}

// NextInterval calculates scheduling interval in days from stability and requested retention.
func (f *FSRS) NextInterval(stability float64) int {
	if stability <= 0 {
		return 1
	}
	// Interval = (stability / factor) * (R^(-2) - 1)
	intvl := (stability / factor) * (math.Pow(f.RequestRetention, -2.0) - 1.0)
	rounded := int(math.Round(intvl))
	if rounded < 1 {
		rounded = 1
	}
	if rounded > f.MaximumInterval {
		rounded = f.MaximumInterval
	}
	return rounded
}

// NextStatesPreview holds predicted scheduling preview for all 4 ratings.
type NextStatesPreview struct {
	Again PreviewGrade `json:"again"`
	Hard  PreviewGrade `json:"hard"`
	Good  PreviewGrade `json:"good"`
	Easy  PreviewGrade `json:"easy"`
}

type PreviewGrade struct {
	Days       int    `json:"days"`
	Label      string `json:"label"`
	Stability  float64 `json:"stability"`
	Difficulty float64 `json:"difficulty"`
}

// PreviewRatings predicts next intervals for all four rating buttons.
func (f *FSRS) PreviewRatings(card CardRecord, now time.Time) NextStatesPreview {
	grades := map[Rating]*PreviewGrade{
		RatingAgain: {},
		RatingHard:  {},
		RatingGood:  {},
		RatingEasy:  {},
	}

	for r := RatingAgain; r <= RatingEasy; r++ {
		res := f.Review(card, r, now)
		days := int(math.Round(res.ScheduledDays))
		label := formatInterval(days)
		grades[r].Days = days
		grades[r].Label = label
		grades[r].Stability = math.Round(res.Card.Stability*100) / 100
		grades[r].Difficulty = math.Round(res.Card.Difficulty*100) / 100
	}

	return NextStatesPreview{
		Again: *grades[RatingAgain],
		Hard:  *grades[RatingHard],
		Good:  *grades[RatingGood],
		Easy:  *grades[RatingEasy],
	}
}

// Review updates a card given a rating and current review timestamp.
func (f *FSRS) Review(card CardRecord, rating Rating, now time.Time) ReviewResult {
	if rating < RatingAgain || rating > RatingEasy {
		rating = RatingGood
	}

	var elapsedDays float64
	if card.LastReviewedAt != nil {
		diff := now.Sub(*card.LastReviewedAt).Hours() / 24.0
		if diff > 0 {
			elapsedDays = diff
		}
	}

	updated := card
	updated.Reps++
	nowCopy := now
	updated.LastReviewedAt = &nowCopy

	var scheduledDays float64

	switch card.State {
	case StateNew:
		updated.Difficulty = f.InitialDifficulty(rating)
		updated.Stability = f.InitialStability(rating)

		if rating == RatingAgain {
			updated.State = StateLearning
			updated.Lapses++
			scheduledDays = 1.0 // 1 day or short session
		} else if rating == RatingHard {
			updated.State = StateLearning
			scheduledDays = 1.0
		} else {
			updated.State = StateReview
			days := f.NextInterval(updated.Stability)
			if rating == RatingEasy && days < 4 {
				days = 4
			}
			scheduledDays = float64(days)
		}

	case StateLearning, StateRelearning:
		if rating == RatingAgain {
			updated.Lapses++
			updated.Stability = f.InitialStability(RatingAgain)
			scheduledDays = 1.0
		} else if rating == RatingHard {
			scheduledDays = 1.0
		} else {
			updated.State = StateReview
			updated.Difficulty = f.NextDifficulty(card.Difficulty, rating)
			if card.Stability <= 0 {
				updated.Stability = f.InitialStability(rating)
			}
			days := f.NextInterval(updated.Stability)
			if rating == RatingEasy && days < 4 {
				days = 4
			}
			scheduledDays = float64(days)
		}

	case StateReview:
		retrievability := f.Retrievability(elapsedDays, card.Stability)
		updated.Difficulty = f.NextDifficulty(card.Difficulty, rating)

		if rating == RatingAgain {
			updated.Lapses++
			updated.State = StateRelearning
			updated.Stability = f.NextForgetStability(card.Difficulty, card.Stability, retrievability)
			scheduledDays = 1.0
		} else {
			updated.Stability = f.NextRecallStability(card.Difficulty, card.Stability, retrievability, rating)
			days := f.NextInterval(updated.Stability)

			// Ensure monotonic progression
			if rating == RatingHard {
				if float64(days) <= elapsedDays {
					days = int(math.Ceil(elapsedDays)) + 1
				}
			} else if rating == RatingEasy {
				goodStability := f.NextRecallStability(card.Difficulty, card.Stability, retrievability, RatingGood)
				goodDays := f.NextInterval(goodStability)
				if days <= goodDays {
					days = goodDays + 1
				}
			}
			scheduledDays = float64(days)
		}
	}

	if scheduledDays < 1.0 {
		scheduledDays = 1.0
	}
	if scheduledDays > float64(f.MaximumInterval) {
		scheduledDays = float64(f.MaximumInterval)
	}

	nextDuration := time.Duration(scheduledDays*24) * time.Hour
	updated.DueAt = now.Add(nextDuration)

	return ReviewResult{
		Card:          updated,
		Rating:        rating,
		ScheduledDays: scheduledDays,
		ElapsedDays:   elapsedDays,
		ReviewedAt:    now,
		NextInterval:  nextDuration,
	}
}

func clamp(val, min, max float64) float64 {
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}

func formatInterval(days int) string {
	if days <= 1 {
		return "1d"
	}
	if days < 30 {
		return fmt.Sprintf("%dd", days)
	}
	if days < 365 {
		m := int(math.Round(float64(days) / 30.4))
		if m <= 0 {
			m = 1
		}
		return fmt.Sprintf("%dmo", m)
	}
	y := int(math.Round(float64(days) / 365.0))
	if y <= 0 {
		y = 1
	}
	return fmt.Sprintf("%dy", y)
}

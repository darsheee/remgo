package srs

import (
	"testing"
	"time"
)

func TestInitialReviews(t *testing.T) {
	scheduler := New()
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		rating        Rating
		expectedState State
		minDays       float64
	}{
		{RatingAgain, StateLearning, 1.0},
		{RatingHard, StateLearning, 1.0},
		{RatingGood, StateReview, 2.0},
		{RatingEasy, StateReview, 4.0},
	}

	for _, tt := range tests {
		card := CardRecord{
			ID:    "card-1",
			State: StateNew,
		}

		res := scheduler.Review(card, tt.rating, now)
		if res.Card.State != tt.expectedState {
			t.Errorf("Rating %s: expected state %s, got %s", tt.rating, tt.expectedState, res.Card.State)
		}
		if res.ScheduledDays < tt.minDays {
			t.Errorf("Rating %s: expected scheduled days >= %f, got %f", tt.rating, tt.minDays, res.ScheduledDays)
		}
		if res.Card.Reps != 1 {
			t.Errorf("expected Reps == 1, got %d", res.Card.Reps)
		}
		if tt.rating == RatingAgain && res.Card.Lapses != 1 {
			t.Errorf("expected Lapses == 1 for Again, got %d", res.Card.Lapses)
		}
	}
}

func TestProgressionAndLapse(t *testing.T) {
	scheduler := New()
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)

	// 1. Initial Good review
	card := CardRecord{ID: "card-2", State: StateNew}
	res1 := scheduler.Review(card, RatingGood, now)
	if res1.Card.State != StateReview {
		t.Fatalf("expected StateReview, got %s", res1.Card.State)
	}
	s1 := res1.Card.Stability

	// 2. Second Good review after scheduled interval
	review2Time := now.Add(time.Duration(res1.ScheduledDays*24) * time.Hour)
	res2 := scheduler.Review(res1.Card, RatingGood, review2Time)
	s2 := res2.Card.Stability
	if s2 <= s1 {
		t.Errorf("expected stability to increase from %f, got %f", s1, s2)
	}
	if res2.ScheduledDays <= res1.ScheduledDays {
		t.Errorf("expected scheduled days to increase: %f -> %f", res1.ScheduledDays, res2.ScheduledDays)
	}

	// 3. Lapse (Again) after lapse
	review3Time := review2Time.Add(time.Duration(res2.ScheduledDays*24) * time.Hour)
	res3 := scheduler.Review(res2.Card, RatingAgain, review3Time)
	if res3.Card.State != StateRelearning {
		t.Errorf("expected StateRelearning on lapse, got %s", res3.Card.State)
	}
	if res3.Card.Lapses != 1 {
		t.Errorf("expected 1 lapse, got %d", res3.Card.Lapses)
	}
	if res3.ScheduledDays != 1.0 {
		t.Errorf("expected lapse interval of 1 day, got %f", res3.ScheduledDays)
	}
}

func TestPreviewRatings(t *testing.T) {
	scheduler := New()
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	lastRev := now.Add(-10 * 24 * time.Hour)
	card := CardRecord{
		ID:             "card-preview",
		State:          StateReview,
		Stability:      10.0,
		Difficulty:     5.0,
		LastReviewedAt: &lastRev,
		DueAt:          now,
	}

	preview := scheduler.PreviewRatings(card, now)
	if preview.Again.Days != 1 {
		t.Errorf("expected Again preview days = 1, got %d", preview.Again.Days)
	}
	if preview.Good.Days <= preview.Hard.Days {
		t.Errorf("expected Good days (%d) > Hard days (%d)", preview.Good.Days, preview.Hard.Days)
	}
	if preview.Easy.Days <= preview.Good.Days {
		t.Errorf("expected Easy days (%d) > Good days (%d)", preview.Easy.Days, preview.Good.Days)
	}
}

func TestRetrievability(t *testing.T) {
	scheduler := New()
	// At t=0, retrievability should be 1.0
	r0 := scheduler.Retrievability(0, 10)
	if r0 != 1.0 {
		t.Errorf("expected r0 = 1.0, got %f", r0)
	}

	// At t=10 and S=10, retrievability should be around 0.90
	r10 := scheduler.Retrievability(10, 10)
	if r10 < 0.88 || r10 > 0.92 {
		t.Errorf("expected r10 around 0.90, got %f", r10)
	}

	// At t=100 and S=10, retrievability should be significantly lower
	r100 := scheduler.Retrievability(100, 10)
	if r100 >= r10 {
		t.Errorf("expected r100 < r10, got %f vs %f", r100, r10)
	}
}

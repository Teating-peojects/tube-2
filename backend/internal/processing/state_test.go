package processing

import (
	"testing"
	"time"
)

func TestTransitionTableAllowsExactlyTheDocumentedMoves(t *testing.T) {
	allowed := map[[2]string]bool{
		{Queued, Processing}: true, {Queued, Failed}: true,
		{Processing, Completed}: true, {Processing, Failed}: true, {Processing, Queued}: true,
	}
	all := []string{Queued, Processing, Completed, Failed, "BOGUS"}
	for _, from := range all {
		for _, to := range all {
			if got, want := CanTransition(from, to), allowed[[2]string{from, to}]; got != want {
				t.Errorf("CanTransition(%s, %s) = %v, want %v", from, to, got, want)
			}
		}
	}
}

func TestBackoffGrowsExponentiallyAndIsCapped(t *testing.T) {
	one := func() float64 { return 0.999999999 }
	base, max := 30*time.Second, 15*time.Minute
	prev := time.Duration(0)
	for attempt := 1; attempt <= 12; attempt++ {
		d := Backoff(attempt, base, max, one)
		if d > max {
			t.Fatalf("attempt %d: %v exceeds the cap %v", attempt, d, max)
		}
		if d < prev {
			t.Fatalf("attempt %d: %v is smaller than the previous %v", attempt, d, prev)
		}
		prev = d
	}
	if d := Backoff(1, base, max, one); d < 29*time.Second || d > 30*time.Second {
		t.Fatalf("first delay should be about the base, got %v", d)
	}
	if d := Backoff(2, base, max, one); d < 59*time.Second || d > 60*time.Second {
		t.Fatalf("second delay should be about twice the base, got %v", d)
	}
	if d := Backoff(30, base, max, one); d < 14*time.Minute+59*time.Second {
		t.Fatalf("a late attempt should sit at the cap, got %v", d)
	}
}

func TestBackoffJitterAndFloor(t *testing.T) {
	if d := Backoff(5, time.Minute, time.Hour, func() float64 { return 0 }); d != time.Second {
		t.Fatalf("zero jitter must still wait one second, got %v", d)
	}
	if d := Backoff(0, time.Minute, time.Hour, func() float64 { return 0.5 }); d != 30*time.Second {
		t.Fatalf("attempt < 1 counts as 1, got %v", d)
	}
	lo, hi := time.Hour, time.Duration(0)
	for i := 0; i < 500; i++ {
		d := Backoff(3, time.Minute, time.Hour, nil)
		lo, hi = min(lo, d), max(hi, d)
		if d < time.Second || d > 4*time.Minute {
			t.Fatalf("delay %v is outside [1s, 4m]", d)
		}
	}
	if hi-lo < time.Minute {
		t.Fatalf("the delays do not look jittered: min %v max %v", lo, hi)
	}
}

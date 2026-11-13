package telegram

import (
	"context"
	"testing"
	"time"
)

// fakeClock lets the limiter be driven without waiting in real time: every
// sleep the limiter asks for moves the clock forward and is recorded.
type fakeClock struct {
	now    time.Time
	slepts []time.Duration
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) Sleep(_ context.Context, d time.Duration) error {
	c.slepts = append(c.slepts, d)
	c.now = c.now.Add(d)
	return nil
}

func (c *fakeClock) attach(l *limiter) *limiter {
	l.now = c.Now
	l.sleep = c.Sleep
	return l
}

func (c *fakeClock) total() time.Duration {
	var sum time.Duration
	for _, d := range c.slepts {
		sum += d
	}
	return sum
}

func TestLimiterWait(t *testing.T) {
	tests := []struct {
		name string
		// calls are the messages sent to one chat, each after advancing the
		// clock by the given gap
		gaps      []time.Duration
		limit     int
		window    time.Duration
		wantSleep int
		wantTotal time.Duration
	}{
		{
			name:   "under the limit nothing waits",
			gaps:   []time.Duration{0, 0},
			limit:  3,
			window: time.Minute,
		},
		{
			name:      "the message over the limit waits out the window",
			gaps:      []time.Duration{0, 0, 0, 0},
			limit:     3,
			window:    time.Minute,
			wantSleep: 1,
			wantTotal: time.Minute,
		},
		{
			name:      "the wait is only what is left of the window",
			gaps:      []time.Duration{0, 20 * time.Second, 0, 0},
			limit:     3,
			window:    time.Minute,
			wantSleep: 1,
			wantTotal: 40 * time.Second,
		},
		{
			name:   "after the window the counter starts over",
			gaps:   []time.Duration{0, 0, 0, time.Minute + time.Second},
			limit:  3,
			window: time.Minute,
		},
		{
			name:      "twenty a minute is the default for a group",
			gaps:      make([]time.Duration, 21),
			limit:     20,
			window:    time.Minute,
			wantSleep: 1,
			wantTotal: time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := newFakeClock()
			l := clock.attach(newLimiter(tt.limit, tt.window))
			for _, gap := range tt.gaps {
				clock.now = clock.now.Add(gap)
				if err := l.wait(context.Background(), -100); err != nil {
					t.Fatalf("wait() error = %v", err)
				}
			}
			if len(clock.slepts) != tt.wantSleep {
				t.Errorf("waited %d times (%v), want %d", len(clock.slepts), clock.slepts, tt.wantSleep)
			}
			if clock.total() != tt.wantTotal {
				t.Errorf("waited %v in total, want %v", clock.total(), tt.wantTotal)
			}
		})
	}
}

// TestLimiterIsPerChat makes sure a busy group does not hold up the other
// chats of the same fan out.
func TestLimiterIsPerChat(t *testing.T) {
	clock := newFakeClock()
	l := clock.attach(newLimiter(2, time.Minute))

	for i := 0; i < 2; i++ {
		if err := l.wait(context.Background(), -100); err != nil {
			t.Fatalf("wait() error = %v", err)
		}
	}
	if err := l.wait(context.Background(), -200); err != nil {
		t.Fatalf("wait() error = %v", err)
	}
	if len(clock.slepts) != 0 {
		t.Fatalf("another chat was held up: %v", clock.slepts)
	}
	if err := l.wait(context.Background(), -100); err != nil {
		t.Fatalf("wait() error = %v", err)
	}
	if len(clock.slepts) != 1 {
		t.Errorf("the busy chat did not wait: %v", clock.slepts)
	}
}

// TestLimiterPause covers the 429 answer: Telegram asks for a pause and the
// chat is left alone for that long.
func TestLimiterPause(t *testing.T) {
	clock := newFakeClock()
	l := clock.attach(newLimiter(20, time.Minute))

	l.pause(-100, 30*time.Second)
	if err := l.wait(context.Background(), -100); err != nil {
		t.Fatalf("wait() error = %v", err)
	}
	if clock.total() != 30*time.Second {
		t.Errorf("waited %v, want the 30s Telegram asked for", clock.total())
	}
	if err := l.wait(context.Background(), -200); err != nil {
		t.Fatalf("wait() error = %v", err)
	}
	if clock.total() != 30*time.Second {
		t.Errorf("the pause spread to another chat: %v", clock.slepts)
	}
}

func TestLimiterRespectsContext(t *testing.T) {
	l := newLimiter(1, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	if err := l.wait(ctx, -100); err != nil {
		t.Fatalf("wait() error = %v", err)
	}
	cancel()
	if err := l.wait(ctx, -100); err == nil {
		t.Error("wait() returned nil, want the cancelled context error")
	}
}

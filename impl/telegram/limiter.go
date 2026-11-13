package telegram

import (
	"context"
	"sync"
	"time"
)

// limiter keeps the number of messages sent to one chat within a window.
// Telegram allows about 20 messages a minute to a group; without this the fan
// out of one event over many chats runs straight into 429 answers.
type limiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	hits   map[int64][]time.Time

	// now and sleep are swapped in tests
	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration) error
}

func newLimiter(limit int, window time.Duration) *limiter {
	if limit <= 0 {
		limit = 20
	}
	if window <= 0 {
		window = time.Minute
	}
	return &limiter{
		limit:  limit,
		window: window,
		hits:   make(map[int64][]time.Time),
		now:    time.Now,
		sleep:  sleepCtx,
	}
}

// wait blocks until one more message may go to a chat, then counts it.
func (l *limiter) wait(ctx context.Context, chatID int64) error {
	for {
		l.mu.Lock()
		now := l.now()
		hits := l.prune(chatID, now)
		if len(hits) < l.limit {
			l.hits[chatID] = append(hits, now)
			l.mu.Unlock()
			return nil
		}
		delay := hits[0].Add(l.window).Sub(now)
		l.mu.Unlock()
		if delay <= 0 {
			delay = time.Millisecond
		}
		if err := l.sleep(ctx, delay); err != nil {
			return err
		}
	}
}

// pause records that a chat must be left alone for a while, after Telegram
// answered with 429 and a retry_after.
func (l *limiter) pause(chatID int64, d time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	hits := l.prune(chatID, now)
	// fill the window so nothing goes to this chat until the pause is over
	blocked := now.Add(d).Add(-l.window)
	for len(hits) < l.limit {
		hits = append(hits, blocked)
	}
	l.hits[chatID] = hits
}

// prune drops the hits that have left the window. The caller holds the lock.
func (l *limiter) prune(chatID int64, now time.Time) []time.Time {
	hits := l.hits[chatID]
	edge := now.Add(-l.window)
	kept := hits[:0]
	for _, hit := range hits {
		if hit.After(edge) {
			kept = append(kept, hit)
		}
	}
	l.hits[chatID] = kept
	return kept
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

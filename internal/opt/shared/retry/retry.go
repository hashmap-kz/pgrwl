package retry

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"
)

var ErrNilOperation = errors.New("retry operation is nil")

type Policy struct {
	// MaxAttempts limits the number of tries.
	// If MaxAttempts <= 0, retry forever until ctx is cancelled.
	MaxAttempts int

	BaseDelay time.Duration
	MaxDelay  time.Duration

	// Jitter adds random delay from 0 to Jitter.
	// Example: Jitter = 250 * time.Millisecond.
	Jitter time.Duration

	// RetryIf decides whether an error should be retried.
	// If nil, all errors are retried.
	RetryIf func(error) bool
}

func Do[T any](ctx context.Context, policy Policy, op func(context.Context) (T, error)) (T, error) {
	var zero T

	if ctx == nil {
		ctx = context.Background()
	}

	if op == nil {
		return zero, ErrNilOperation
	}

	policy = policy.withDefaults()

	var lastErr error

	for attempt := 1; ; attempt++ {
		result, err := op(ctx)
		if err == nil {
			return result, nil
		}

		lastErr = err

		if ctx.Err() != nil {
			return zero, ctx.Err()
		}

		if policy.RetryIf != nil && !policy.RetryIf(err) {
			return zero, err
		}

		if policy.MaxAttempts > 0 && attempt >= policy.MaxAttempts {
			return zero, fmt.Errorf("retry failed after %d attempts: %w", attempt, lastErr)
		}

		delay := policy.delay(attempt)

		if err := sleepContext(ctx, delay); err != nil {
			return zero, err
		}
	}
}

func (p Policy) withDefaults() Policy {
	if p.BaseDelay <= 0 {
		p.BaseDelay = 100 * time.Millisecond
	}

	if p.MaxDelay <= 0 {
		p.MaxDelay = 5 * time.Second
	}

	if p.MaxDelay < p.BaseDelay {
		p.MaxDelay = p.BaseDelay
	}

	return p
}

func (p Policy) delay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}

	delay := p.BaseDelay

	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= p.MaxDelay {
			delay = p.MaxDelay
			break
		}
	}

	if p.Jitter > 0 {
		delay += time.Duration(rand.Int64N(int64(p.Jitter)))
	}

	return delay
}

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

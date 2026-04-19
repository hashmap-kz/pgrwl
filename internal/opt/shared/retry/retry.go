package retry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"time"
)

var ErrNilOperation = errors.New("retry operation is nil")

var discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

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

	// Logger is optional.
	// If nil, logs are discarded.
	Logger *slog.Logger
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
	loggr := policy.logger()

	var lastErr error

	for attempt := 1; ; attempt++ {
		loggr.Debug("retry attempt started",
			slog.Int("attempt", attempt),
			slog.Int("max_attempts", policy.MaxAttempts),
		)

		result, err := op(ctx)
		if err == nil {
			loggr.Debug("retry attempt succeeded",
				slog.Int("attempt", attempt),
			)

			return result, nil
		}

		lastErr = err

		loggr.Debug("retry attempt failed",
			slog.Int("attempt", attempt),
			slog.Any("err", err),
		)

		if ctx.Err() != nil {
			loggr.Debug("retry stopped because context is done",
				slog.Int("attempt", attempt),
				slog.Any("err", ctx.Err()),
			)

			return zero, ctx.Err()
		}

		if policy.RetryIf != nil && !policy.RetryIf(err) {
			loggr.Debug("retry stopped because error is not retryable",
				slog.Int("attempt", attempt),
				slog.Any("err", err),
			)

			return zero, err
		}

		if policy.MaxAttempts > 0 && attempt >= policy.MaxAttempts {
			loggr.Debug("retry stopped because max attempts reached",
				slog.Int("attempt", attempt),
				slog.Int("max_attempts", policy.MaxAttempts),
				slog.Any("err", lastErr),
			)

			return zero, fmt.Errorf("retry failed after %d attempts: %w", attempt, lastErr)
		}

		delay := policy.delay(attempt)

		loggr.Debug("retry sleeping before next attempt",
			slog.Int("attempt", attempt),
			slog.Duration("delay", delay),
		)

		if err := sleepContext(ctx, delay); err != nil {
			loggr.Debug("retry sleep interrupted",
				slog.Int("attempt", attempt),
				slog.Any("err", err),
			)

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

func (p Policy) logger() *slog.Logger {
	if p.Logger != nil {
		return p.Logger
	}
	return discardLogger
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
		//nolint:gosec
		delay += time.Duration(rand.Int64N(int64(p.Jitter)))
	}

	return delay
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

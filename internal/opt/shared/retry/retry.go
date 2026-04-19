package retry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"
)

var ErrNilOperation = errors.New("retry operation is nil")

var discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

type Policy struct {
	// MaxAttempts limits the number of tries.
	//
	// If MaxAttempts <= 0, Do retries forever until ctx is cancelled
	// or RetryIf rejects an error.
	MaxAttempts int

	// Delay is the wait time between failed attempts.
	//
	// If Delay <= 0, a small default delay is used.
	Delay time.Duration

	// RetryIf decides whether an error should be retried.
	//
	// If RetryIf is nil, all errors are retried.
	RetryIf func(error) bool

	// Logger is optional.
	//
	// If Logger is nil, logs are discarded.
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

		loggr.Debug("retry sleeping before next attempt",
			slog.Int("attempt", attempt),
			slog.Duration("delay", policy.Delay),
		)

		if err := sleepContext(ctx, policy.Delay); err != nil {
			loggr.Debug("retry sleep interrupted",
				slog.Int("attempt", attempt),
				slog.Any("err", err),
			)

			return zero, err
		}
	}
}

func (p Policy) withDefaults() Policy {
	if p.Delay <= 0 {
		p.Delay = 100 * time.Millisecond
	}

	return p
}

func (p Policy) logger() *slog.Logger {
	if p.Logger != nil {
		return p.Logger
	}

	return discardLogger
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

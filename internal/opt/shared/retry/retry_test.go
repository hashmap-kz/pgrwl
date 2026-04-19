package retry

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDoSuccessFirstAttempt(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	attempts := 0

	got, err := Do(ctx, Policy{
		MaxAttempts: 3,
		BaseDelay:   time.Millisecond,
		MaxDelay:    time.Millisecond,
	}, func(context.Context) (string, error) {
		attempts++
		return "ok", nil
	})

	assert.NoError(t, err)
	assert.Equal(t, "ok", got)
	assert.Equal(t, 1, attempts)
}

func TestDoRetriesUntilSuccess(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	attempts := 0
	temporaryErr := errors.New("temporary")

	got, err := Do(ctx, Policy{
		MaxAttempts: 5,
		BaseDelay:   time.Millisecond,
		MaxDelay:    time.Millisecond,
	}, func(context.Context) (int, error) {
		attempts++

		if attempts < 3 {
			return 0, temporaryErr
		}

		return 42, nil
	})

	assert.NoError(t, err)
	assert.Equal(t, 42, got)
	assert.Equal(t, 3, attempts)
}

func TestDoStopsAfterMaxAttempts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	boom := errors.New("boom")
	attempts := 0

	got, err := Do(ctx, Policy{
		MaxAttempts: 3,
		BaseDelay:   time.Millisecond,
		MaxDelay:    time.Millisecond,
	}, func(context.Context) (string, error) {
		attempts++
		return "", boom
	})

	assert.Error(t, err)
	assert.ErrorIs(t, err, boom)
	assert.Empty(t, got)
	assert.Equal(t, 3, attempts)
}

func TestDoRetryForeverUntilSuccess(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	attempts := 0
	temporaryErr := errors.New("temporary")

	got, err := Do(ctx, Policy{
		MaxAttempts: 0,
		BaseDelay:   time.Millisecond,
		MaxDelay:    time.Millisecond,
	}, func(context.Context) (string, error) {
		attempts++

		if attempts < 5 {
			return "", temporaryErr
		}

		return "connected", nil
	})

	assert.NoError(t, err)
	assert.Equal(t, "connected", got)
	assert.Equal(t, 5, attempts)
}

func TestDoStopsWhenRetryIfReturnsFalse(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	permanentErr := errors.New("permanent")
	attempts := 0

	got, err := Do(ctx, Policy{
		MaxAttempts: 10,
		BaseDelay:   time.Millisecond,
		MaxDelay:    time.Millisecond,
		RetryIf: func(err error) bool {
			return !errors.Is(err, permanentErr)
		},
	}, func(context.Context) (string, error) {
		attempts++
		return "", permanentErr
	})

	assert.ErrorIs(t, err, permanentErr)
	assert.Empty(t, got)
	assert.Equal(t, 1, attempts)
}

func TestDoLogsAttemptsWhenLoggerProvided(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	loggr := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	attempts := 0
	temporaryErr := errors.New("temporary")

	got, err := Do(context.Background(), Policy{
		MaxAttempts: 3,
		BaseDelay:   time.Millisecond,
		MaxDelay:    time.Millisecond,
		Logger:      loggr,
	}, func(context.Context) (string, error) {
		attempts++

		if attempts < 3 {
			return "", temporaryErr
		}

		return "ok", nil
	})

	assert.NoError(t, err)
	assert.Equal(t, "ok", got)
	assert.Equal(t, 3, attempts)

	logs := buf.String()

	assert.Contains(t, logs, "retry attempt started")
	assert.Contains(t, logs, "retry attempt failed")
	assert.Contains(t, logs, "retry sleeping before next attempt")
	assert.Contains(t, logs, "retry attempt succeeded")
	assert.Contains(t, logs, "attempt=1")
	assert.Contains(t, logs, "attempt=2")
	assert.Contains(t, logs, "attempt=3")
}

func TestDoWithNilLoggerDoesNotPanic(t *testing.T) {
	t.Parallel()

	attempts := 0

	got, err := Do(context.Background(), Policy{
		MaxAttempts: 2,
		BaseDelay:   time.Millisecond,
		MaxDelay:    time.Millisecond,
		Logger:      nil,
	}, func(context.Context) (int, error) {
		attempts++
		return 42, nil
	})

	assert.NoError(t, err)
	assert.Equal(t, 42, got)
	assert.Equal(t, 1, attempts)
}

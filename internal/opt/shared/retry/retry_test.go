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
		Delay:       time.Millisecond,
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
		Delay:       time.Millisecond,
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
		Delay:       time.Millisecond,
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
		Delay:       time.Millisecond,
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
		Delay:       time.Millisecond,
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
		Delay:       time.Millisecond,
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
		Delay:       time.Millisecond,
		Logger:      nil,
	}, func(context.Context) (int, error) {
		attempts++
		return 42, nil
	})

	assert.NoError(t, err)
	assert.Equal(t, 42, got)
	assert.Equal(t, 1, attempts)
}

func TestDoNilOperation(t *testing.T) {
	t.Parallel()

	got, err := Do[string](context.Background(), Policy{}, nil)

	assert.ErrorIs(t, err, ErrNilOperation)
	assert.Empty(t, got)
}

func TestDoNilContextUsesBackground(t *testing.T) {
	t.Parallel()

	attempts := 0

	//nolint:staticcheck
	got, err := Do[string](nil, Policy{
		MaxAttempts: 1,
		Delay:       time.Millisecond,
	}, func(ctx context.Context) (string, error) {
		attempts++

		assert.NotNil(t, ctx)

		return "ok", nil
	})

	assert.NoError(t, err)
	assert.Equal(t, "ok", got)
	assert.Equal(t, 1, attempts)
}

func TestDoStopsWhenContextCancelledAfterAttempt(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	attempts := 0
	temporaryErr := errors.New("temporary")

	got, err := Do(ctx, Policy{
		MaxAttempts: 0,
		Delay:       time.Hour,
	}, func(context.Context) (string, error) {
		attempts++
		cancel()

		return "", temporaryErr
	})

	assert.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, got)
	assert.Equal(t, 1, attempts)
}

func TestDoStopsWhenContextDeadlineExceededDuringSleep(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	attempts := 0
	temporaryErr := errors.New("temporary")

	start := time.Now()

	got, err := Do(ctx, Policy{
		MaxAttempts: 0,
		Delay:       time.Second,
	}, func(context.Context) (string, error) {
		attempts++
		return "", temporaryErr
	})

	elapsed := time.Since(start)

	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Empty(t, got)
	assert.Equal(t, 1, attempts)
	assert.Less(t, elapsed, 500*time.Millisecond)
}

func TestDoUsesDefaultDelay(t *testing.T) {
	t.Parallel()

	policy := Policy{}.withDefaults()

	assert.Equal(t, 100*time.Millisecond, policy.Delay)
}

func TestDoPreservesConfiguredDelay(t *testing.T) {
	t.Parallel()

	policy := Policy{
		Delay: 250 * time.Millisecond,
	}.withDefaults()

	assert.Equal(t, 250*time.Millisecond, policy.Delay)
}

func TestDoCanWrapConnectionFunction(t *testing.T) {
	t.Parallel()

	type fakeConn struct {
		id int
	}

	ctx := context.Background()

	attempts := 0
	temporaryErr := errors.New("postgres is not ready")

	conn, err := Do(ctx, Policy{
		MaxAttempts: 3,
		Delay:       time.Millisecond,
	}, func(context.Context) (*fakeConn, error) {
		attempts++

		if attempts < 2 {
			return nil, temporaryErr
		}

		return &fakeConn{id: 123}, nil
	})

	assert.NoError(t, err)
	assert.NotNil(t, conn)
	assert.Equal(t, 123, conn.id)
	assert.Equal(t, 2, attempts)
}

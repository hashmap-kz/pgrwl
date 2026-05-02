package backupsv

import (
	"context"
	"testing"
	"time"

	"github.com/pgrwl/pgrwl/config"
	st "github.com/pgrwl/pgrwl/internal/opt/shared/storecrypt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNoopRetentionReturnsContextErrorOnly(t *testing.T) {
	ctx := context.Background()
	assert.NoError(t, NoopRetention{}.RunBeforeBackup(ctx, testStartupInfo()))

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	assert.ErrorIs(t, NoopRetention{}.RunBeforeBackup(canceled, testStartupInfo()), context.Canceled)
}

func TestNewRetentionServiceReturnsNoopWhenConfigNilOrDisabled(t *testing.T) {
	backend := st.NewInMemoryStorage()
	walStor := newPlainVariadicStorage(t, backend)

	assert.IsType(t, NoopRetention{}, NewRetentionService(nil, &Opts{}, testLogger(), backend, walStor))
	assert.IsType(t, NoopRetention{}, NewRetentionService(&config.Config{}, &Opts{}, testLogger(), backend, walStor))
}

func TestConfiguredRetentionReturnsContextErrorBeforeDoingWork(t *testing.T) {
	backend := st.NewInMemoryStorage()
	walStor := newPlainVariadicStorage(t, backend)
	retention := &ConfiguredRetention{
		l:              testLogger(),
		cfg:            retentionConfigForTest(),
		opts:           &Opts{},
		basebackupStor: backend,
		walStor:        walStor,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := retention.RunBeforeBackup(ctx, testStartupInfo())

	assert.ErrorIs(t, err, context.Canceled)
}

func TestConfiguredRetentionDisabledReturnsNil(t *testing.T) {
	backend := st.NewInMemoryStorage()
	walStor := newPlainVariadicStorage(t, backend)
	retention := &ConfiguredRetention{
		l:              testLogger(),
		cfg:            &config.Config{Retention: config.RetentionConfig{Enable: false}},
		opts:           &Opts{},
		basebackupStor: backend,
		walStor:        walStor,
	}

	err := retention.RunBeforeBackup(context.Background(), testStartupInfo())

	require.NoError(t, err)
}

func TestConfiguredRetentionUnsupportedTypeReturnsError(t *testing.T) {
	backend := st.NewInMemoryStorage()
	walStor := newPlainVariadicStorage(t, backend)
	retention := &ConfiguredRetention{
		l: testLogger(),
		cfg: &config.Config{Retention: config.RetentionConfig{
			Enable:             true,
			Type:               "unknown",
			KeepDurationParsed: time.Hour,
		}},
		opts:           &Opts{},
		basebackupStor: backend,
		walStor:        walStor,
	}

	err := retention.RunBeforeBackup(context.Background(), testStartupInfo())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported backup retention type")
}

func TestConfiguredRetentionRecoveryWindowWithEmptyStorageSucceeds(t *testing.T) {
	backend := st.NewInMemoryStorage()
	walStor := newPlainVariadicStorage(t, backend)
	retention := &ConfiguredRetention{
		l:              testLogger(),
		cfg:            retentionConfigForTest(),
		opts:           &Opts{},
		basebackupStor: backend,
		walStor:        walStor,
	}

	err := retention.RunBeforeBackup(context.Background(), testStartupInfo())

	require.NoError(t, err)
}

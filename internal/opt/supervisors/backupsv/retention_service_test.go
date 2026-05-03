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
	assert.NoError(t, NoopRetention{}.RunBeforeBackup(ctx))

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	assert.ErrorIs(t, NoopRetention{}.RunBeforeBackup(canceled), context.Canceled)
}

func TestNewRetentionServiceReturnsNoopWhenConfigNilOrDisabled(t *testing.T) {
	assert.IsType(t, NoopRetention{}, NewRetentionService(&BackupSupervisorOpts{}))
	assert.IsType(t, NoopRetention{}, NewRetentionService(&BackupSupervisorOpts{}))
}

func TestConfiguredRetentionReturnsContextErrorBeforeDoingWork(t *testing.T) {
	retention := &ConfiguredRetention{
		l:    testLogger(),
		opts: &BackupSupervisorOpts{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := retention.RunBeforeBackup(ctx)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestConfiguredRetentionDisabledReturnsNil(t *testing.T) {
	backend := st.NewInMemoryStorage()
	walStor := newPlainVariadicStorage(t, backend)
	retention := &ConfiguredRetention{
		l: testLogger(),
		opts: &BackupSupervisorOpts{
			WalStor: walStor,
			Cfg:     &config.Config{Retention: config.RetentionConfig{Enable: false}},
		},
	}

	err := retention.RunBeforeBackup(context.Background())

	require.NoError(t, err)
}

func TestConfiguredRetentionUnsupportedTypeReturnsError(t *testing.T) {
	backend := st.NewInMemoryStorage()
	walStor := newPlainVariadicStorage(t, backend)
	retention := &ConfiguredRetention{
		l: testLogger(),
		opts: &BackupSupervisorOpts{
			WalStor: walStor,
			Cfg: &config.Config{Retention: config.RetentionConfig{
				Enable:             true,
				Type:               "unknown",
				KeepDurationParsed: time.Hour,
			}},
		},
	}

	err := retention.RunBeforeBackup(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported backup retention type")
}

func TestConfiguredRetentionRecoveryWindowWithEmptyStorageSucceeds(t *testing.T) {
	backend := st.NewInMemoryStorage()
	walStor := newPlainVariadicStorage(t, backend)
	retention := &ConfiguredRetention{
		l: testLogger(),
		opts: &BackupSupervisorOpts{
			WalStor: walStor,
			Cfg:     &config.Config{},
		},
	}

	err := retention.RunBeforeBackup(context.Background())

	require.NoError(t, err)
}

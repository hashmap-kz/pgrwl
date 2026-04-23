package combinedmode

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pgrwl/pgrwl/internal/core/xlog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fakes

type fakeService struct {
	state  xlog.ReceiverState
	status *xlog.StreamStatus
	// startErr is returned by ReceiverStart if non-nil.
	startErr error
	// walErr is returned by GetWalFile if non-nil.
	walErr  error
	walBody string // content returned by GetWalFile on success
}

func (f *fakeService) ReceiverStart() error               { return f.startErr }
func (f *fakeService) ReceiverStop()                      {}
func (f *fakeService) ReceiverState() xlog.ReceiverState  { return f.state }
func (f *fakeService) ReceiverStatus() *xlog.StreamStatus { return f.status }
func (f *fakeService) GetWalFile(_ context.Context, _ string) (io.ReadCloser, error) {
	if f.walErr != nil {
		return nil, f.walErr
	}
	return io.NopCloser(strings.NewReader(f.walBody)), nil
}

func newController(svc Service) *Controller {
	return &Controller{Service: svc}
}

// GET /receiver

func TestGetReceiver_Running(t *testing.T) {
	svc := &fakeService{
		state: xlog.ReceiverStateRunning,
		status: &xlog.StreamStatus{
			Running:      true,
			Slot:         "pgrwl",
			LastFlushLSN: "0/3000000",
		},
	}
	ctrl := newController(svc)

	req := httptest.NewRequest(http.MethodGet, "/receiver", nil)
	rec := httptest.NewRecorder()
	ctrl.GetReceiverHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp ReceiverStateResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, xlog.ReceiverStateRunning, resp.State)
	require.NotNil(t, resp.StreamStatus)
	assert.True(t, resp.StreamStatus.Running)
	assert.Equal(t, "pgrwl", resp.StreamStatus.Slot)
}

func TestGetReceiver_Stopped(t *testing.T) {
	svc := &fakeService{
		state:  xlog.ReceiverStateStopped,
		status: &xlog.StreamStatus{Running: false},
	}
	ctrl := newController(svc)

	req := httptest.NewRequest(http.MethodGet, "/receiver", nil)
	rec := httptest.NewRecorder()
	ctrl.GetReceiverHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp ReceiverStateResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, xlog.ReceiverStateStopped, resp.State)
	assert.False(t, resp.StreamStatus.Running)
}

// POST /receiver

func TestSetReceiver_StartSuccess(t *testing.T) {
	svc := &fakeService{
		state:  xlog.ReceiverStateRunning,
		status: &xlog.StreamStatus{Running: true},
	}
	ctrl := newController(svc)

	body := `{"state":"running"}`
	req := httptest.NewRequest(http.MethodPost, "/receiver", strings.NewReader(body))
	rec := httptest.NewRecorder()
	ctrl.SetReceiverHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp ReceiverStateResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, xlog.ReceiverStateRunning, resp.State)
}

func TestSetReceiver_StopSuccess(t *testing.T) {
	svc := &fakeService{
		state:  xlog.ReceiverStateStopped,
		status: &xlog.StreamStatus{Running: false},
	}
	ctrl := newController(svc)

	body := `{"state":"stopped"}`
	req := httptest.NewRequest(http.MethodPost, "/receiver", strings.NewReader(body))
	rec := httptest.NewRecorder()
	ctrl.SetReceiverHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp ReceiverStateResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, xlog.ReceiverStateStopped, resp.State)
}

func TestSetReceiver_StartConflict(t *testing.T) {
	svc := &fakeService{
		startErr: assert.AnError,
		state:    xlog.ReceiverStateRunning,
		status:   &xlog.StreamStatus{Running: true},
	}
	ctrl := newController(svc)

	body := `{"state":"running"}`
	req := httptest.NewRequest(http.MethodPost, "/receiver", strings.NewReader(body))
	rec := httptest.NewRecorder()
	ctrl.SetReceiverHandler(rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestSetReceiver_InvalidState(t *testing.T) {
	ctrl := newController(&fakeService{})

	body := `{"state":"banana"}`
	req := httptest.NewRequest(http.MethodPost, "/receiver", strings.NewReader(body))
	rec := httptest.NewRecorder()
	ctrl.SetReceiverHandler(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestSetReceiver_MalformedJSON(t *testing.T) {
	ctrl := newController(&fakeService{})

	req := httptest.NewRequest(http.MethodPost, "/receiver", strings.NewReader("{bad json"))
	rec := httptest.NewRecorder()
	ctrl.SetReceiverHandler(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// GET /wal/{filename}

func TestWalDownload_Found(t *testing.T) {
	svc := &fakeService{walBody: "WALDATA"}
	ctrl := newController(svc)

	req := httptest.NewRequest(http.MethodGet, "/wal/000000010000000000000001", nil)
	req.SetPathValue("filename", "000000010000000000000001")
	rec := httptest.NewRecorder()
	ctrl.WalFileDownloadHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "WALDATA", rec.Body.String())
}

func TestWalDownload_NotFound(t *testing.T) {
	svc := &fakeService{walErr: assert.AnError}
	ctrl := newController(svc)

	req := httptest.NewRequest(http.MethodGet, "/wal/missing", nil)
	req.SetPathValue("filename", "missing")
	rec := httptest.NewRecorder()
	ctrl.WalFileDownloadHandler(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestWalDownload_MissingPathParam(t *testing.T) {
	ctrl := newController(&fakeService{})

	req := httptest.NewRequest(http.MethodGet, "/wal/", nil)
	// deliberately do NOT call req.SetPathValue - simulates missing param
	rec := httptest.NewRecorder()
	ctrl.WalFileDownloadHandler(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

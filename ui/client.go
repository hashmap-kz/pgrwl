package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Client interface {
	Snapshot(ctx context.Context, receiver Receiver) Snapshot
}

type HTTPClient struct {
	HTTP *http.Client
}

func NewHTTPClient() *HTTPClient {
	return &HTTPClient{
		HTTP: &http.Client{Timeout: 3 * time.Second},
	}
}

func (c *HTTPClient) Snapshot(ctx context.Context, receiver Receiver) Snapshot {
	s := Snapshot{Receiver: receiver}

	status, err := getJSON[PgrwlStatus](ctx, c.http(), receiver.Addr, "/api/v1/status")
	if err != nil {
		s.Error = fmt.Sprintf("cannot reach receiver at %s: %v", receiver.Addr, err)
	} else {
		s.Status = &status
	}

	cfg, err := getJSON[BriefConfig](ctx, c.http(), receiver.Addr, "/api/v1/brief-config")
	if err == nil {
		s.Config = &cfg
	}

	wal, err := getJSON[[]WALFile](ctx, c.http(), receiver.Addr, "/api/v1/wals")
	if err == nil {
		s.WALFiles = wal
	}

	backups, err := getJSON[[]Backup](ctx, c.http(), receiver.Addr, "/api/v1/backups")
	if err == nil {
		s.Backups = backups
	}

	return s
}

func (c *HTTPClient) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func getJSON[T any](ctx context.Context, client *http.Client, baseURL, path string) (T, error) {
	var zero T

	baseURL = strings.TrimRight(baseURL, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
	if err != nil {
		return zero, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return zero, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return zero, fmt.Errorf("%s returned %s", path, resp.Status)
	}

	if err := json.NewDecoder(resp.Body).Decode(&zero); err != nil {
		return zero, err
	}
	return zero, nil
}

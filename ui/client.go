package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
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

// Snapshot fetches all four endpoints concurrently and merges results.
// If /status fails the Snapshot.Error field is set; other fields are
// populated independently so a partial view is still rendered.
func (c *HTTPClient) Snapshot(ctx context.Context, receiver Receiver) Snapshot {
	s := Snapshot{Receiver: receiver}

	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)

	launch := func(fn func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fn()
		}()
	}

	launch(func() {
		status, err := getJSON[PgrwlStatus](ctx, c.http(), receiver.Addr, "/api/v1/status")
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			s.Error = fmt.Sprintf("cannot reach receiver at %s: %v", receiver.Addr, err)
		} else {
			s.Status = &status
		}
	})

	launch(func() {
		cfg, err := getJSON[BriefConfig](ctx, c.http(), receiver.Addr, "/api/v1/brief-config")
		if err == nil {
			mu.Lock()
			s.Config = &cfg
			mu.Unlock()
		}
	})

	launch(func() {
		wal, err := getJSON[[]WALFile](ctx, c.http(), receiver.Addr, "/api/v1/wals")
		if err == nil {
			mu.Lock()
			s.WALFiles = wal
			mu.Unlock()
		}
	})

	launch(func() {
		backups, err := getJSON[[]Backup](ctx, c.http(), receiver.Addr, "/api/v1/backups")
		if err == nil {
			mu.Lock()
			s.Backups = backups
			mu.Unlock()
		}
	})

	wg.Wait()
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

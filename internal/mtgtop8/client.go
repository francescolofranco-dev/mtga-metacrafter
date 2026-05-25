package mtgtop8

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	BaseURL = "https://mtgtop8.com"
	// MetaLargeEvents is MTGTop8's curated "Large Events Last 2 Months" view.
	// Drops local store events and casual MTGO Leagues; keeps Regional
	// Championships, RCQs, MTGO Challenges, MTGO Showcase Challenges, etc.
	MetaLargeEvents = "46"
	userAgent       = "mtga-metacrafter/0.2 (+https://github.com/francescolofranco-dev/mtga-metacrafter) - polite tournament-data fetcher"
)

// Client is a polite HTTP client for MTGTop8.
type Client struct {
	HTTP    *http.Client
	BaseURL string
	MinGap  time.Duration
	Logger  *slog.Logger

	mu       sync.Mutex
	lastSent time.Time
}

func NewClient(logger *slog.Logger) *Client {
	return &Client{
		HTTP:    &http.Client{Timeout: 30 * time.Second},
		BaseURL: BaseURL,
		MinGap:  1 * time.Second,
		Logger:  logger,
	}
}

// Get fetches a path and returns the body, respecting the rate limit.
func (c *Client) Get(ctx context.Context, path string) ([]byte, error) {
	if err := c.wait(ctx); err != nil {
		return nil, err
	}

	url := c.BaseURL
	if !strings.HasPrefix(path, "http") {
		if !strings.HasPrefix(path, "/") {
			url += "/"
		}
		url += path
	} else {
		url = path
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mtgtop8 get %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mtgtop8 %s: status %d", url, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func (c *Client) wait(ctx context.Context) error {
	c.mu.Lock()
	gap := time.Since(c.lastSent)
	wait := c.MinGap - gap
	c.lastSent = time.Now().Add(maxDur(0, wait))
	c.mu.Unlock()
	if wait <= 0 {
		return nil
	}
	select {
	case <-time.After(wait):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func maxDur(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

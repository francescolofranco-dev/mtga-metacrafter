package mtggoldfish

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
	BaseURL   = "https://www.mtggoldfish.com"
	userAgent = "mtga-metacrafter/0.1 (+https://github.com/francescolofranco-dev/mtga-metacrafter) - polite weekly meta scraper"
)

// Client is a polite HTTP client for MTGGoldfish.
// All requests share a minimum gap (default 1s) to be a good citizen.
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

// Get fetches a path (with or without a leading "/") and returns the body.
// It blocks until MinGap has passed since the previous request.
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
		return nil, fmt.Errorf("mtggoldfish get %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mtggoldfish %s: status %d", url, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func (c *Client) wait(ctx context.Context) error {
	c.mu.Lock()
	gap := time.Since(c.lastSent)
	wait := c.MinGap - gap
	c.lastSent = time.Now().Add(maxDuration(0, wait))
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

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

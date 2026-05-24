package scryfall

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	BaseURL   = "https://api.scryfall.com"
	userAgent = "mtga-metacrafter/0.1 (+https://github.com/francescolofranco-dev/mtga-metacrafter)"
)

// Client fetches card data from Scryfall.
//
// We use the bulk-data endpoint (oracle_cards) rather than the paginated
// search API: one large download instead of 20+ small requests, which is
// both faster overall and much friendlier to Scryfall's rate limits.
type Client struct {
	HTTP    *http.Client
	BaseURL string
	Logger  *slog.Logger
}

func NewClient(logger *slog.Logger) *Client {
	return &Client{
		HTTP:    &http.Client{Timeout: 5 * time.Minute}, // bulk file is ~30 MB
		BaseURL: BaseURL,
		Logger:  logger,
	}
}

// FetchLegalInAny returns every oracle card that is legal in at least one
// of the listed formats (e.g. "standard", "pioneer", "modern").
func (c *Client) FetchLegalInAny(ctx context.Context, formats []string) ([]*Card, error) {
	t0 := time.Now()

	bulkURL, err := c.findBulkURL(ctx, "oracle_cards")
	if err != nil {
		return nil, fmt.Errorf("scryfall bulk metadata: %w", err)
	}
	c.Logger.Info("scryfall bulk url resolved", "type", "oracle_cards", "url", bulkURL, "formats", formats)

	cards, total, err := c.fetchLegalFromBulk(ctx, bulkURL, formats)
	if err != nil {
		return nil, err
	}
	c.Logger.Info("scryfall fetch complete",
		"cards_total", total,
		"cards_kept", len(cards),
		"elapsed", time.Since(t0).String())
	return cards, nil
}

type bulkListResponse struct {
	Data []bulkFile `json:"data"`
}

type bulkFile struct {
	Type        string `json:"type"`
	DownloadURI string `json:"download_uri"`
	UpdatedAt   string `json:"updated_at"`
}

func (c *Client) findBulkURL(ctx context.Context, wantType string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/bulk-data", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}
	var b bulkListResponse
	if err := json.NewDecoder(resp.Body).Decode(&b); err != nil {
		return "", err
	}
	for _, f := range b.Data {
		if f.Type == wantType {
			return f.DownloadURI, nil
		}
	}
	return "", fmt.Errorf("bulk type %q not found", wantType)
}

type rawCard struct {
	Name       string            `json:"name"`
	Rarity     string            `json:"rarity"`
	ManaCost   string            `json:"mana_cost"`
	TypeLine   string            `json:"type_line"`
	Set        string            `json:"set"`
	ImageURIs  map[string]string `json:"image_uris"`
	CardFaces  []rawCardFace     `json:"card_faces"`
	Legalities map[string]string `json:"legalities"`
}

type rawCardFace struct {
	Name      string            `json:"name"`
	ManaCost  string            `json:"mana_cost"`
	ImageURIs map[string]string `json:"image_uris"`
}

// fetchLegalFromBulk stream-decodes the oracle_cards JSON array, keeping
// cards legal in at least one of the listed formats. Returns (kept, total_seen, error).
func (c *Client) fetchLegalFromBulk(ctx context.Context, url string, formats []string) ([]*Card, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("download status %d", resp.StatusCode)
	}

	dec := json.NewDecoder(resp.Body)
	tok, err := dec.Token()
	if err != nil {
		return nil, 0, fmt.Errorf("decode head: %w", err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '[' {
		return nil, 0, fmt.Errorf("expected JSON array, got %v", tok)
	}

	var out []*Card
	total := 0
	for dec.More() {
		var r rawCard
		if err := dec.Decode(&r); err != nil {
			return nil, total, fmt.Errorf("decode card %d: %w", total, err)
		}
		total++
		if !legalInAny(r.Legalities, formats) {
			continue
		}
		out = append(out, convert(r))
	}
	return out, total, nil
}

func legalInAny(legalities map[string]string, formats []string) bool {
	for _, f := range formats {
		if legalities[f] == "legal" {
			return true
		}
	}
	return false
}

func convert(r rawCard) *Card {
	c := &Card{
		Name:     r.Name,
		Rarity:   r.Rarity,
		ManaCost: r.ManaCost,
		TypeLine: r.TypeLine,
		Set:      r.Set,
		ImageURI: pickImage(r.ImageURIs),
	}
	for _, f := range r.CardFaces {
		c.Faces = append(c.Faces, f.Name)
		if c.ImageURI == "" {
			c.ImageURI = pickImage(f.ImageURIs)
		}
		if c.ManaCost == "" {
			c.ManaCost = f.ManaCost
		}
	}
	return c
}

func pickImage(m map[string]string) string {
	for _, k := range []string{"normal", "small", "large", "border_crop"} {
		if v, ok := m[k]; ok && v != "" {
			return v
		}
	}
	return ""
}

// Index is a name-based lookup over a set of cards. It indexes the full name,
// each face name (for DFC/split/adventure cards), and the front-face name of
// "A // B"-style full names — sources like MTGGoldfish refer to such cards
// by either spelling.
type Index struct {
	byName map[string]*Card
}

func BuildIndex(cards []*Card) *Index {
	idx := &Index{byName: make(map[string]*Card, len(cards)*2)}
	for _, c := range cards {
		idx.add(c.Name, c)
		for _, f := range c.Faces {
			idx.add(f, c)
		}
		if i := strings.Index(c.Name, " // "); i > 0 {
			idx.add(c.Name[:i], c)
		}
	}
	return idx
}

func (idx *Index) add(name string, c *Card) {
	key := normalize(name)
	if key == "" {
		return
	}
	if _, ok := idx.byName[key]; !ok {
		idx.byName[key] = c
	}
}

func (idx *Index) Lookup(name string) (*Card, bool) {
	c, ok := idx.byName[normalize(name)]
	return c, ok
}

func (idx *Index) Len() int { return len(idx.byName) }

func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

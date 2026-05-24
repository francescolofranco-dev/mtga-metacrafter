package mtggoldfish

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// FetchDeck pulls the mainboard for one deck via /deck/visual/<id>.
//
// We use the visual page instead of /deck/<id> because the latter sits behind
// Cloudflare's bot challenge while the visual variant is publicly cacheable.
func (c *Client) FetchDeck(ctx context.Context, deckURL string) (*DeckCards, error) {
	body, err := c.Get(ctx, deckURL)
	if err != nil {
		return nil, err
	}
	cards, err := ParseDeckVisual(body)
	if err != nil {
		return nil, err
	}
	return &DeckCards{DeckURL: deckURL, Cards: cards}, nil
}

// ParseDeckVisual extracts mainboard cards + quantities from a deck-visual page.
//
// The visual layout renders one <img class='deck-visual-pile-card …' alt='Card
// Name'> per copy. We count alt values up to the sideboard separator
// (<div class='deck-visual-playmat-sideboard'>).
func ParseDeckVisual(html []byte) ([]DeckCard, error) {
	// Split off the sideboard before parsing so we can't accidentally pick up
	// sideboard cards.
	mainHTML := stripSideboard(html)

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(mainHTML))
	if err != nil {
		return nil, fmt.Errorf("parse deck visual: %w", err)
	}

	counts := map[string]int{}
	doc.Find("img.deck-visual-pile-card").Each(func(_ int, img *goquery.Selection) {
		alt, _ := img.Attr("alt")
		alt = strings.TrimSpace(alt)
		if alt == "" {
			return
		}
		counts[alt]++
	})

	if len(counts) == 0 {
		return nil, fmt.Errorf("parse deck visual: no cards found")
	}

	out := make([]DeckCard, 0, len(counts))
	for name, n := range counts {
		out = append(out, DeckCard{Name: name, Quantity: n})
	}
	return out, nil
}

func stripSideboard(html []byte) []byte {
	marker := []byte(`class='deck-visual-playmat-sideboard'`)
	if i := bytes.Index(html, marker); i > 0 {
		return html[:i]
	}
	marker2 := []byte(`class="deck-visual-playmat-sideboard"`)
	if i := bytes.Index(html, marker2); i > 0 {
		return html[:i]
	}
	return html
}

package mtgtop8

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// FetchDeck loads a single deck's mainboard from its URL.
func (c *Client) FetchDeck(ctx context.Context, deckURL string) (*DeckCards, error) {
	body, err := c.Get(ctx, deckURL)
	if err != nil {
		return nil, err
	}
	cards, err := ParseDeck(body)
	if err != nil {
		return nil, err
	}
	return &DeckCards{URL: deckURL, Cards: cards}, nil
}

// ParseDeck extracts mainboard cards + quantities from an MTGTop8 deck page.
//
// MTGTop8 renders each card line as:
//
//	<div id="mdXXX###" class="deck_line ..." onclick="…">NUM <span class="L14">Card Name</span></div>
//
// Mainboard lines have the `md` prefix, sideboard lines have `sb`. We only
// keep mainboard.
func ParseDeck(html []byte) ([]DeckCard, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("parse deck: %w", err)
	}

	counts := map[string]int{}

	doc.Find("div.deck_line").Each(func(_ int, line *goquery.Selection) {
		id, _ := line.Attr("id")
		if !strings.HasPrefix(id, "md") {
			return
		}
		nameSel := line.Find("span.L14").First()
		name := strings.TrimSpace(nameSel.Text())
		if name == "" {
			return
		}
		// The quantity is the text inside the div BEFORE the <span>.
		// Easiest way: full text minus the name = quantity-as-string.
		full := strings.TrimSpace(line.Text())
		qtyStr := strings.TrimSpace(strings.TrimSuffix(full, name))
		qty := parseLeadingInt(qtyStr)
		if qty <= 0 {
			return
		}
		counts[name] += qty
	})

	if len(counts) == 0 {
		return nil, fmt.Errorf("parse deck: no mainboard cards found")
	}

	out := make([]DeckCard, 0, len(counts))
	for name, qty := range counts {
		out = append(out, DeckCard{Name: name, Quantity: qty})
	}
	return out, nil
}

var leadingIntRE = regexp.MustCompile(`^\s*([0-9]+)`)

func parseLeadingInt(s string) int {
	m := leadingIntRE.FindStringSubmatch(s)
	if len(m) < 2 {
		return 0
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0
	}
	return n
}

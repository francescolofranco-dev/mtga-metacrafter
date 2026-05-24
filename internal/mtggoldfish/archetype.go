package mtggoldfish

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

func (c *Client) FetchArchetype(ctx context.Context, url string) ([]BreakdownEntry, error) {
	body, err := c.Get(ctx, url)
	if err != nil {
		return nil, err
	}
	return ParseArchetype(body)
}

// ParseArchetype extracts the per-card breakdown for an archetype page.
//
// The .deck-archetype-breakdown div contains one .spoiler-card-container per
// section (Creatures, Spells, Lands, Sideboard …). Each container starts with
// an <h3>Section Name</h3> followed by one or more .spoiler-card entries.
// Containers whose h3 is "Sideboard" are intentionally excluded so the score
// reflects mainboard play only.
//
// Each .spoiler-card has:
//   - <span class='price-card-invisible-label'>Card Name</span>
//   - <p class='archetype-breakdown-featured-card-text'> "3.9 in 98% of decks" </p>
func ParseArchetype(html []byte) ([]BreakdownEntry, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("parse archetype: %w", err)
	}

	breakdown := doc.Find(".deck-archetype-breakdown").First()
	if breakdown.Length() == 0 {
		return nil, fmt.Errorf("parse archetype: .deck-archetype-breakdown not found")
	}

	var out []BreakdownEntry

	breakdown.Find(".spoiler-card-container").Each(func(_ int, container *goquery.Selection) {
		section := strings.TrimSpace(container.Find("h3").First().Text())
		if strings.EqualFold(section, "Sideboard") {
			return
		}
		container.Find(".spoiler-card").Each(func(_ int, card *goquery.Selection) {
			name := strings.TrimSpace(card.Find(".price-card-invisible-label").First().Text())
			text := strings.TrimSpace(card.Find(".archetype-breakdown-featured-card-text").First().Text())
			avg, incl, ok := parseBreakdownText(text)
			if name == "" || !ok {
				return
			}
			out = append(out, BreakdownEntry{
				CardName:     name,
				AvgCopies:    avg,
				InclusionPct: incl,
			})
		})
	})

	if len(out) == 0 {
		return nil, fmt.Errorf("parse archetype: no breakdown entries found")
	}
	return out, nil
}

// e.g. "3.9 in 98% of decks" or "4.0 in 100% of decks"
var breakdownRE = regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?)\s+in\s+([0-9]+(?:\.[0-9]+)?)\s*%`)

func parseBreakdownText(s string) (avg float64, incl float64, ok bool) {
	m := breakdownRE.FindStringSubmatch(s)
	if len(m) < 3 {
		return 0, 0, false
	}
	avg, err1 := strconv.ParseFloat(m[1], 64)
	incl, err2 := strconv.ParseFloat(m[2], 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return avg, incl, true
}

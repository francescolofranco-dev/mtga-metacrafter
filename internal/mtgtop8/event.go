package mtgtop8

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// FetchEvent loads the standings table for an event URL.
func (c *Client) FetchEvent(ctx context.Context, eventURL string) ([]DeckStanding, error) {
	body, err := c.Get(ctx, eventURL)
	if err != nil {
		return nil, err
	}
	return ParseEvent(body, c.BaseURL)
}

// ParseEvent extracts the per-finisher rows from an event page.
//
// Each finisher row is a flex container with three children:
//   - <div class=S14 width=42px> with the placement ("1", "2", "3-4", "5-8")
//   - <a href=?e=...&d=...&f=...><img>  (thumbnail link, repeats deck URL)
//   - <a href=?e=...&d=...&f=...>Archetype Name</a>
//
// We use the archetype-text link as the canonical row anchor.
func ParseEvent(html []byte, baseURL string) ([]DeckStanding, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("parse event: %w", err)
	}

	var standings []DeckStanding
	seen := map[string]bool{}

	// Iterate all <a> tags whose href looks like a deck-detail relative URL.
	doc.Find("a").Each(func(_ int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		// Deck detail links look like "?e=85553&d=849796&f=ST" — relative to
		// the current /event page.
		if !strings.HasPrefix(href, "?") || !strings.Contains(href, "d=") {
			return
		}
		text := strings.TrimSpace(a.Text())
		if text == "" {
			return // skip the thumbnail link (no text)
		}
		deckID := extractDeckIDQuery(href)
		if deckID == "" || seen[deckID] {
			return
		}
		seen[deckID] = true

		// Place is the first .S14 sibling div before this anchor's parent.
		// Walk up to the flex container then find the position div.
		place := ""
		row := a.ParentsFiltered("div[class*='hover_tr'], div[class*='chosen_tr']").First()
		if row.Length() > 0 {
			row.Find("div.S14[align='center']").EachWithBreak(func(_ int, d *goquery.Selection) bool {
				txt := strings.TrimSpace(d.Text())
				if isPlaceLabel(txt) {
					place = txt
					return false
				}
				return true
			})
		}

		standings = append(standings, DeckStanding{
			Place:     place,
			Archetype: text,
			URL:       joinURL(baseURL, "/event?"+strings.TrimPrefix(href, "?")),
			DeckID:    deckID,
		})
	})

	return standings, nil
}

var deckIDQueryRE = regexp.MustCompile(`d=([0-9]+)`)

func extractDeckIDQuery(href string) string {
	m := deckIDQueryRE.FindStringSubmatch(href)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// isPlaceLabel rejects obvious non-place strings. MTGTop8 uses "1", "2",
// "3-4", "5-8", "9-16", "17-32", "T8", etc.
func isPlaceLabel(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && r != '-' && r != 'T' && r != 't' {
			return false
		}
	}
	return true
}

// BuildDeckURL composes a /event URL for a specific deck inside an event.
func BuildDeckURL(baseURL, eventID, deckID, formatCode string) string {
	v := url.Values{}
	v.Set("e", eventID)
	v.Set("d", deckID)
	v.Set("f", formatCode)
	return joinURL(baseURL, "/event?"+v.Encode())
}

package mtggoldfish

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// FetchTournaments fetches /tournaments/<format> and returns the events.
func (c *Client) FetchTournaments(ctx context.Context, formatSlug string) ([]TournamentEvent, error) {
	body, err := c.Get(ctx, "/tournaments/"+formatSlug)
	if err != nil {
		return nil, err
	}
	return ParseTournaments(body, c.BaseURL)
}

// ParseTournaments extracts event blocks from a tournaments index page.
//
// MTGGoldfish wraps the entire list in a single .similar-events-container.
// Inside that container, each tournament is rendered as an <h4> header
// followed by a sibling <table> with the standings:
//
//	<h4>
//	  <a aria-label="N stars">…</a>           (optional — sets the tier)
//	  <a href="/tournament/<id>">Event title</a>
//	  <nobr>on YYYY-MM-DD</nobr>
//	</h4>
//	<table class='table-similar-events'>
//	  <tr><td class='column-place'>1st</td>
//	      <td class='column-deck'><a href="/deck/<id>#online">Archetype</a> …</td>
//	  </tr>
//	</table>
//
// MTGO leagues (titles starting with "Standard League", "Modern League", …) are
// dropped — the user-facing intent is "important tournaments", not 5-0 dumps.
func ParseTournaments(html []byte, baseURL string) ([]TournamentEvent, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("parse tournaments: %w", err)
	}

	container := doc.Find(".tournaments-recent-events .similar-events-container").First()
	if container.Length() == 0 {
		return nil, fmt.Errorf("parse tournaments: .similar-events-container not found")
	}

	var out []TournamentEvent

	container.Find("h4").Each(func(_ int, h *goquery.Selection) {
		title := strings.TrimSpace(h.Find("a[href*='/tournament/']").First().Text())
		href, _ := h.Find("a[href*='/tournament/']").First().Attr("href")
		if title == "" || href == "" {
			return
		}
		if isLeagueTitle(title) {
			return
		}

		stars := 0
		if attr, ok := h.Find("a[aria-label$=' stars']").First().Attr("aria-label"); ok {
			stars = parseStarTier(attr)
		}

		date := parseDateFromNobr(h.Find("nobr").First().Text())

		// The table for this event is the next <table> sibling.
		table := h.NextAllFiltered("table").First()
		if table.Length() == 0 {
			return
		}

		var standings []DeckStanding
		table.Find("tbody tr").Each(func(_ int, row *goquery.Selection) {
			place := strings.TrimSpace(row.Find("td.column-place").First().Text())
			var arch, deckHref string
			row.Find("td.column-deck a[href*='/deck/']").EachWithBreak(func(_ int, a *goquery.Selection) bool {
				txt := strings.TrimSpace(a.Text())
				h, _ := a.Attr("href")
				if txt != "" && h != "" {
					arch = txt
					deckHref = h
					return false
				}
				return true
			})
			if arch == "" || deckHref == "" {
				return
			}
			id := extractDeckID(deckHref)
			if id == "" {
				return
			}
			standings = append(standings, DeckStanding{
				Place:     place,
				Archetype: arch,
				DeckURL:   joinURL(baseURL, "/deck/visual/"+id),
				DeckID:    id,
			})
		})

		if len(standings) == 0 {
			return
		}

		out = append(out, TournamentEvent{
			Title:     title,
			URL:       joinURL(baseURL, href),
			Date:      date,
			StarTier:  stars,
			Standings: standings,
		})
	})

	return out, nil
}

func isLeagueTitle(t string) bool {
	low := strings.ToLower(t)
	return strings.Contains(low, " league ") ||
		strings.HasSuffix(low, " league") ||
		strings.HasPrefix(low, "league ")
}

var starRE = regexp.MustCompile(`^([0-9]+)\s+stars?$`)

func parseStarTier(ariaLabel string) int {
	m := starRE.FindStringSubmatch(strings.TrimSpace(ariaLabel))
	if len(m) < 2 {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	if n < 0 {
		return 0
	}
	if n > 5 {
		return 5
	}
	return n
}

var dateRE = regexp.MustCompile(`(\d{4}-\d{2}-\d{2})`)

func parseDateFromNobr(s string) time.Time {
	m := dateRE.FindStringSubmatch(s)
	if len(m) < 2 {
		return time.Time{}
	}
	t, err := time.Parse("2006-01-02", m[1])
	if err != nil {
		return time.Time{}
	}
	return t
}

// Match /deck/123, /deck/visual/456, /deck/arena/789, with optional #fragment.
var deckIDRE = regexp.MustCompile(`/deck/(?:visual/|arena/)?([0-9]+)`)

func extractDeckID(href string) string {
	m := deckIDRE.FindStringSubmatch(href)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

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

// FetchTournaments fetches /tournaments/<format> (page 1) and returns the events.
func (c *Client) FetchTournaments(ctx context.Context, formatSlug string) ([]TournamentEvent, error) {
	return c.FetchTournamentsPage(ctx, formatSlug, 1)
}

// FetchTournamentStandings pulls every visible deck row from a tournament's
// detail page (/tournament/<id>). The tournaments index only shows ~4–8 rows
// per event inline; the detail page exposes the full top-100-ish standings.
func (c *Client) FetchTournamentStandings(ctx context.Context, tournamentURL string) ([]DeckStanding, error) {
	body, err := c.Get(ctx, tournamentURL)
	if err != nil {
		return nil, err
	}
	return ParseTournamentDetailStandings(body, c.BaseURL)
}

// ParseTournamentDetailStandings extracts standings rows from a tournament
// detail page.
//
// Layout:
//
//	<tr class='tournament-decklist-odd'>     (or 'tournament-decklist-event')
//	  <td>1st</td>
//	  <td><a href="/deck/<id>">Archetype</a> <span class='manacost'/></td>
//	  <td><a href="/player/...">Player</a></td>
//	  …prices…
//	  <td data-deckId='<id>'><a>Expand</a></td>
//	</tr>
//
// The hidden expand row (class 'tournament-decklist') sits right after each
// visible row and we ignore it.
func ParseTournamentDetailStandings(html []byte, baseURL string) ([]DeckStanding, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("parse tournament detail: %w", err)
	}

	var out []DeckStanding
	seen := map[string]bool{}

	doc.Find("tr.tournament-decklist-odd, tr.tournament-decklist-event").Each(func(_ int, row *goquery.Selection) {
		place := strings.TrimSpace(row.Find("td").First().Text())
		link := row.Find("a[href*='/deck/']").First()
		if link.Length() == 0 {
			return
		}
		arch := strings.TrimSpace(link.Text())
		href, _ := link.Attr("href")
		id := extractDeckID(href)
		if id == "" || arch == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, DeckStanding{
			Place:     place,
			Archetype: arch,
			DeckURL:   joinURL(baseURL, "/deck/visual/"+id),
			DeckID:    id,
		})
	})

	return out, nil
}

// FetchTournamentsPage fetches a specific page of the tournaments index.
// MTGGoldfish supports ?page=N for older results.
func (c *Client) FetchTournamentsPage(ctx context.Context, formatSlug string, page int) ([]TournamentEvent, error) {
	path := "/tournaments/" + formatSlug
	if page > 1 {
		path += "?page=" + itoa(page)
	}
	body, err := c.Get(ctx, path)
	if err != nil {
		return nil, err
	}
	return ParseTournaments(body, c.BaseURL)
}

func itoa(n int) string {
	return strconv.Itoa(n)
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
		stars := 0
		if attr, ok := h.Find("a[aria-label$=' stars']").First().Attr("aria-label"); ok {
			stars = parseStarTier(attr)
		}

		date := parseDateFromNobr(h.Find("nobr").First().Text())
		league := isLeagueTitle(title)

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
			IsLeague:  league,
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

package mtgtop8

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// FetchFormatIndex returns the most-recent large-tier events for `format`.
//
// We hit /format with `meta=46` ("Large Events Last 2 Months"), which already
// filters out MTGO Leagues and casual store events. The remaining tiering is
// derived from the event title via TierFromTitle.
func (c *Client) FetchFormatIndex(ctx context.Context, format FormatSpec) ([]TournamentEvent, error) {
	q := url.Values{}
	q.Set("f", format.Code)
	q.Set("meta", MetaLargeEvents)
	body, err := c.Get(ctx, "/format?"+q.Encode())
	if err != nil {
		return nil, err
	}
	return ParseFormatIndex(body, c.BaseURL, format.Code)
}

// ParseFormatIndex extracts the deduplicated event list from a format page.
//
// Each event is rendered as a table row containing:
//
//	<td width=70% class=S14><a href=event?e=NNNNN&f=ST>Event Name</a> [@ Location] [NEW]</td>
//	…
//	<td align=right width=12% class=S12>DD/MM/YY</td>
//
// The same event ID typically appears once per featured archetype, so we
// dedupe by event ID and keep the first occurrence.
func ParseFormatIndex(html []byte, baseURL string, formatCode string) ([]TournamentEvent, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("parse format index: %w", err)
	}

	seen := map[string]bool{}
	var events []TournamentEvent

	doc.Find("a[href^='event?e=']").Each(func(_ int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		title := strings.TrimSpace(a.Text())
		if title == "" {
			return
		}
		id := extractEventID(href)
		if id == "" || seen[id] {
			return
		}

		// Date is in the same row as this link — find the closest enclosing
		// table row and look for a date cell.
		var date time.Time
		row := a.ParentsFiltered("tr").First()
		if row.Length() > 0 {
			row.Find("td.S12").EachWithBreak(func(_ int, td *goquery.Selection) bool {
				if d, ok := parseEUDate(strings.TrimSpace(td.Text())); ok {
					date = d
					return false
				}
				return true
			})
		}
		seen[id] = true
		events = append(events, TournamentEvent{
			Title:      title,
			URL:        joinURL(baseURL, "/event?e="+id+"&f="+formatCode),
			EventID:    id,
			Date:       date,
			TierWeight: TierFromTitle(title),
		})
	})

	return events, nil
}

var eventIDRE = regexp.MustCompile(`e=([0-9]+)`)

func extractEventID(href string) string {
	m := eventIDRE.FindStringSubmatch(href)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// parseEUDate parses MTGTop8's DD/MM/YY date strings (e.g. "23/05/26").
func parseEUDate(s string) (time.Time, bool) {
	t, err := time.Parse("02/01/06", s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// TierFromTitle derives a tier weight from the event title. The weights are
// chosen so that:
//   - Pro Tour / Worlds outweigh Regional Championships,
//   - Regional Championships outweigh RCQs and Opens,
//   - MTGO weekend events outweigh weekly MTGO challenges,
//   - everything outweighs casual MTGO Leagues (rare on this view since
//     `meta=46` filters most of them out, but kept for safety).
func TierFromTitle(title string) float64 {
	t := strings.ToLower(title)
	switch {
	case strings.Contains(t, "pro tour"), strings.Contains(t, "world championship"):
		return 5
	case strings.Contains(t, "regional championship"), strings.Contains(t, "magiccon"):
		return 4
	case strings.Contains(t, "$uper $unday"), strings.Contains(t, "rcq"),
		strings.Contains(t, "open ") || strings.HasSuffix(t, " open"),
		strings.Contains(t, "team series"):
		return 3
	case strings.Contains(t, "mtgo showcase"):
		return 2.5
	case strings.Contains(t, "store championship"):
		return 2
	case strings.Contains(t, "mtgo challenge"), strings.Contains(t, "showdown"):
		return 1.5
	case strings.Contains(t, "mtgo league"), strings.Contains(t, " league"):
		return 0.5
	default:
		return 1
	}
}

func joinURL(base, path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	base = strings.TrimSuffix(base, "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}

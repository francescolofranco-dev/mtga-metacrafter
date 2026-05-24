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

const MetaPath = "/metagame/standard"

func (c *Client) FetchMeta(ctx context.Context) ([]Archetype, error) {
	body, err := c.Get(ctx, MetaPath)
	if err != nil {
		return nil, err
	}
	return ParseMeta(body, c.BaseURL)
}

// ParseMeta extracts the archetype list from a metagame page.
//
// Each archetype is rendered as .archetype-tile containing:
//   - .archetype-tile-title with one or more <a href="/archetype/...#online|#paper">Name</a>
//   - .archetype-tile-statistic.metagame-percentage > .archetype-tile-statistic-value
//     with text "11.9%\n(630)" — the value before % is the metashare.
func ParseMeta(html []byte, baseURL string) ([]Archetype, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("parse meta: %w", err)
	}

	var out []Archetype
	seen := map[string]bool{}

	doc.Find(".archetype-tile").Each(func(_ int, tile *goquery.Selection) {
		link := tile.Find(".archetype-tile-title a").First()
		name := strings.TrimSpace(link.Text())
		href, _ := link.Attr("href")
		if name == "" || href == "" {
			return
		}

		// Strip the "#online"/"#paper" fragment.
		if i := strings.Index(href, "#"); i >= 0 {
			href = href[:i]
		}
		// Skip duplicates (paper and online tiles often appear together).
		if seen[href] {
			return
		}
		seen[href] = true

		shareText := tile.Find(".metagame-percentage .archetype-tile-statistic-value").First().Text()
		share := extractPercent(shareText)
		if share == 0 {
			return
		}

		out = append(out, Archetype{
			Name:         name,
			MetasharePct: share,
			URL:          joinURL(baseURL, href),
		})
	})

	if len(out) == 0 {
		return nil, fmt.Errorf("parse meta: no archetypes found (selectors likely changed)")
	}
	return out, nil
}

var percentRE = regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?)\s*%`)

func extractPercent(s string) float64 {
	m := percentRE.FindStringSubmatch(s)
	if len(m) < 2 {
		return 0
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	return v
}

func joinURL(base, path string) string {
	if strings.HasPrefix(path, "http") {
		return path
	}
	base = strings.TrimSuffix(base, "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}

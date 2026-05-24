package mtggoldfish

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseTournaments_RealFixture(t *testing.T) {
	html := mustReadFixture(t, "tournaments_standard.html")
	events, err := ParseTournaments(html, "https://www.mtggoldfish.com")
	if err != nil {
		t.Fatalf("ParseTournaments: %v", err)
	}
	if len(events) < 3 {
		t.Fatalf("expected >= 3 non-League events, got %d", len(events))
	}

	// No League events should slip through.
	for _, e := range events {
		if isLeagueTitle(e.Title) {
			t.Errorf("league event leaked into output: %q", e.Title)
		}
	}

	// First event sanity checks.
	a := events[0]
	if a.URL == "" || a.URL[:8] != "https://" {
		t.Errorf("URL not absolute: %q", a.URL)
	}
	if len(a.Standings) == 0 {
		t.Errorf("first event has no standings")
	}
	for _, s := range a.Standings {
		if s.DeckID == "" {
			t.Errorf("standing missing deck ID: %+v", s)
		}
		if s.Archetype == "" {
			t.Errorf("standing missing archetype: %+v", s)
		}
		if !filepathLike(s.DeckURL, "/deck/visual/") {
			t.Errorf("deck URL not in visual form: %q", s.DeckURL)
		}
	}
}

func TestParseDeckVisual_RealFixture(t *testing.T) {
	html := mustReadFixture(t, "deck_visual.html")
	cards, err := ParseDeckVisual(html)
	if err != nil {
		t.Fatalf("ParseDeckVisual: %v", err)
	}
	if len(cards) < 10 {
		t.Fatalf("expected >= 10 unique cards, got %d", len(cards))
	}

	// Mainboard quantities should sum to ~60 for a Standard deck. Allow some
	// slack for split-card oddities or visual rendering quirks.
	total := 0
	for _, c := range cards {
		if c.Quantity <= 0 || c.Quantity > 4 {
			// Lands legally exceed 4; tighten only the upper-cap.
			if c.Quantity > 20 {
				t.Errorf("implausible quantity for %q: %d", c.Name, c.Quantity)
			}
		}
		total += c.Quantity
	}
	if total < 40 || total > 100 {
		t.Errorf("total mainboard quantity %d is far from 60", total)
	}
}

func TestIsLeagueTitle(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"Standard League 2026-05-24", true},
		{"Modern League 2026-05-24", true},
		{"Standard Challenge 32 2026-05-23", false},
		{"MTG SEA Championship Final", false},
		{"League event banner", true},
	}
	for _, c := range cases {
		if got := isLeagueTitle(c.in); got != c.want {
			t.Errorf("isLeagueTitle(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseStarTier(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"1 stars", 1},
		{"3 stars", 3},
		{"0 stars", 0},
		{"", 0},
		{"garbage", 0},
		{"100 stars", 5}, // clamp
	}
	for _, c := range cases {
		if got := parseStarTier(c.in); got != c.want {
			t.Errorf("parseStarTier(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestExtractDeckID(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"/deck/7799236#online", "7799236"},
		{"/deck/visual/7799236", "7799236"},
		{"https://www.mtggoldfish.com/deck/123", "123"},
		{"/archetype/standard", ""},
	}
	for _, c := range cases {
		if got := extractDeckID(c.in); got != c.want {
			t.Errorf("extractDeckID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func filepathLike(s, want string) bool {
	for i := 0; i+len(want) <= len(s); i++ {
		if s[i:i+len(want)] == want {
			return true
		}
	}
	return false
}

func mustReadFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

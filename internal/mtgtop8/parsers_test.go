package mtgtop8

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseFormatIndex_RealFixture(t *testing.T) {
	html := mustReadFixture(t, "format_index.html")
	events, err := ParseFormatIndex(html, "https://mtgtop8.com", "ST")
	if err != nil {
		t.Fatalf("ParseFormatIndex: %v", err)
	}
	if len(events) < 5 {
		t.Fatalf("expected >= 5 events, got %d", len(events))
	}

	// Sanity: every event has an ID, a URL, and a non-empty title.
	for _, e := range events {
		if e.EventID == "" {
			t.Errorf("event missing EventID: %+v", e)
		}
		if !contains(e.URL, "/event?e=") {
			t.Errorf("event URL not absolute /event?…: %q", e.URL)
		}
		if e.Title == "" {
			t.Errorf("event missing title: %+v", e)
		}
		if e.TierWeight <= 0 {
			t.Errorf("event has non-positive tier: %v %+v", e.TierWeight, e)
		}
	}

	// We expect at least one Regional Championship-grade event in this fixture.
	var maxTier float64
	for _, e := range events {
		if e.TierWeight > maxTier {
			maxTier = e.TierWeight
		}
	}
	if maxTier < 3 {
		t.Errorf("expected at least one tier-3+ event in fixture, got max tier %v", maxTier)
	}
}

func TestParseEvent_RealFixture(t *testing.T) {
	html := mustReadFixture(t, "event.html")
	standings, err := ParseEvent(html, "https://mtgtop8.com")
	if err != nil {
		t.Fatalf("ParseEvent: %v", err)
	}
	if len(standings) < 8 {
		t.Fatalf("expected >= 8 standings, got %d", len(standings))
	}
	for _, s := range standings {
		if s.DeckID == "" {
			t.Errorf("standing missing DeckID: %+v", s)
		}
		if !contains(s.URL, "/event?") || !contains(s.URL, "d=") {
			t.Errorf("standing URL malformed: %q", s.URL)
		}
		if s.Archetype == "" {
			t.Errorf("standing missing archetype label: %+v", s)
		}
	}
}

func TestParseDeck_RealFixture(t *testing.T) {
	html := mustReadFixture(t, "deck.html")
	cards, err := ParseDeck(html)
	if err != nil {
		t.Fatalf("ParseDeck: %v", err)
	}
	if len(cards) < 10 {
		t.Fatalf("expected >= 10 unique mainboard cards, got %d", len(cards))
	}
	total := 0
	for _, c := range cards {
		if c.Quantity <= 0 || c.Quantity > 20 {
			t.Errorf("implausible quantity for %q: %d", c.Name, c.Quantity)
		}
		total += c.Quantity
	}
	if total < 40 || total > 100 {
		t.Errorf("total mainboard quantity %d is far from 60", total)
	}

	// Sideboard cards must NOT leak in.
	for _, c := range cards {
		if c.Name == "Sheltered by Ghosts" {
			t.Errorf("sideboard card %q leaked into mainboard", c.Name)
		}
	}
}

func TestTierFromTitle(t *testing.T) {
	cases := []struct {
		title string
		want  float64
	}{
		{"Pro Tour FFA", 5},
		{"World Championship 30", 5},
		{"Regional Championship - SCG Cincinnati", 4},
		{"MagicCon: Vegas Open", 4},
		{"$uper $unday RCQ", 3},
		{"MTGO Showcase Challenge", 2.5},
		{"Store Championship", 2},
		{"MTGO Challenge 32", 1.5},
		{"MTGO League", 0.5},
		{"FNM Showdown", 1.5},
		{"Random Event", 1},
	}
	for _, c := range cases {
		if got := TierFromTitle(c.title); got != c.want {
			t.Errorf("TierFromTitle(%q) = %v, want %v", c.title, got, c.want)
		}
	}
}

func TestParseEUDate(t *testing.T) {
	cases := []struct {
		in   string
		want string // YYYY-MM-DD
		ok   bool
	}{
		{"23/05/26", "2026-05-23", true},
		{"01/12/24", "2024-12-01", true},
		{"garbage", "", false},
	}
	for _, c := range cases {
		got, ok := parseEUDate(c.in)
		if ok != c.ok {
			t.Errorf("parseEUDate(%q) ok=%v, want %v", c.in, ok, c.ok)
			continue
		}
		if ok && got.Format("2006-01-02") != c.want {
			t.Errorf("parseEUDate(%q) = %s, want %s", c.in, got.Format("2006-01-02"), c.want)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || (len(s) > len(sub) && (s[:len(sub)] == sub || contains(s[1:], sub))))
}

func mustReadFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

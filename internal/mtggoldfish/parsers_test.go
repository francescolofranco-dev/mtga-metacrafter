package mtggoldfish

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseMeta_RealFixture(t *testing.T) {
	html := mustReadFixture(t, "meta_sample.html")
	archs, err := ParseMeta(html, "https://www.mtggoldfish.com")
	if err != nil {
		t.Fatalf("ParseMeta: %v", err)
	}
	if len(archs) < 5 {
		t.Fatalf("expected >= 5 archetypes, got %d", len(archs))
	}

	// Sanity checks on the first archetype.
	a := archs[0]
	if a.Name == "" {
		t.Errorf("first archetype has empty Name")
	}
	if a.URL == "" || a.URL[:8] != "https://" {
		t.Errorf("first archetype URL not absolute: %q", a.URL)
	}
	if a.MetasharePct <= 0 || a.MetasharePct > 100 {
		t.Errorf("first archetype metashare out of range: %v", a.MetasharePct)
	}

	// Metashares should sum to a plausible range (won't be exactly 100 because
	// long-tail archetypes may be omitted, but should be at least 50).
	var sum float64
	for _, a := range archs {
		sum += a.MetasharePct
	}
	if sum < 50 || sum > 150 {
		t.Errorf("sum of metashares unlikely: %v across %d archetypes", sum, len(archs))
	}
}

func TestParseArchetype_RealFixture(t *testing.T) {
	html := mustReadFixture(t, "archetype_sample.html")
	entries, err := ParseArchetype(html)
	if err != nil {
		t.Fatalf("ParseArchetype: %v", err)
	}
	if len(entries) < 10 {
		t.Fatalf("expected >= 10 breakdown entries, got %d", len(entries))
	}

	// No empty names, and no obviously bogus values.
	for _, e := range entries {
		if e.CardName == "" {
			t.Errorf("breakdown entry has empty CardName")
		}
		// Land entries can be much higher (Forest @ 7.4); cap generously.
		if e.AvgCopies <= 0 || e.AvgCopies > 12 {
			t.Errorf("AvgCopies out of range for %q: %v", e.CardName, e.AvgCopies)
		}
		if e.InclusionPct <= 0 || e.InclusionPct > 100 {
			t.Errorf("InclusionPct out of range for %q: %v", e.CardName, e.InclusionPct)
		}
	}

	// "Sideboard" cards must be excluded. The fixture has a sideboard entry for
	// "Sheltered by Ghosts" but no mainboard one. Make sure it doesn't appear.
	for _, e := range entries {
		if e.CardName == "Sheltered by Ghosts" {
			t.Errorf("sideboard card %q leaked into mainboard breakdown", e.CardName)
		}
	}
}

func TestParseBreakdownText(t *testing.T) {
	cases := []struct {
		in       string
		wantAvg  float64
		wantIncl float64
		wantOK   bool
	}{
		{"4.0 in 100% of decks", 4.0, 100, true},
		{"3.9 in 98% of decks", 3.9, 98, true},
		{"  2.0 in 92% of decks  ", 2.0, 92, true},
		{"no numbers here", 0, 0, false},
		{"", 0, 0, false},
	}
	for _, c := range cases {
		avg, incl, ok := parseBreakdownText(c.in)
		if ok != c.wantOK || avg != c.wantAvg || incl != c.wantIncl {
			t.Errorf("parseBreakdownText(%q) = (%v, %v, %v), want (%v, %v, %v)",
				c.in, avg, incl, ok, c.wantAvg, c.wantIncl, c.wantOK)
		}
	}
}

func TestExtractPercent(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"11.9%", 11.9},
		{"11.9%\n(630)", 11.9},
		{"  4 %", 4},
		{"no percent", 0},
		{"", 0},
	}
	for _, c := range cases {
		got := extractPercent(c.in)
		if got != c.want {
			t.Errorf("extractPercent(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func mustReadFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

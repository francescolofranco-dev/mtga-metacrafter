package score

import (
	"testing"

	"github.com/francescolofranco-dev/mtga-metacrafter/internal/mtggoldfish"
	"github.com/francescolofranco-dev/mtga-metacrafter/internal/scryfall"
)

func TestCompute_RanksAndCounts(t *testing.T) {
	cards := scryfall.BuildIndex([]*scryfall.Card{
		{Name: "Llanowar Elves", Rarity: "common", TypeLine: "Creature — Elf Druid"},
		{Name: "Mossborn Hydra", Rarity: "rare", TypeLine: "Creature — Hydra"},
		{Name: "Forest", Rarity: "common", TypeLine: "Basic Land — Forest"},
	})
	archs := []mtggoldfish.Archetype{
		{Name: "Selesnya Landfall", URL: "u1", MetasharePct: 12.0},
		{Name: "Mono-Red", URL: "u2", MetasharePct: 8.0},
	}
	bd := map[string][]mtggoldfish.BreakdownEntry{
		"u1": {
			{CardName: "Llanowar Elves", AvgCopies: 4.0, InclusionPct: 100},
			{CardName: "Mossborn Hydra", AvgCopies: 2.0, InclusionPct: 92},
			{CardName: "Forest", AvgCopies: 7.0, InclusionPct: 100},        // skip: basic land
			{CardName: "Unknown Card", AvgCopies: 3.0, InclusionPct: 90},   // skip: not in index
		},
		"u2": {
			{CardName: "Llanowar Elves", AvgCopies: 1.0, InclusionPct: 30},
		},
	}

	out := Compute(archs, bd, cards)
	if len(out) != 2 {
		t.Fatalf("expected 2 cards (basic land + unknown skipped), got %d", len(out))
	}

	if out[0].Name != "Llanowar Elves" {
		t.Errorf("expected top card Llanowar Elves, got %q", out[0].Name)
	}
	// 12 * 100 * 4.0 / 100  +  8 * 30 * 1.0 / 100  = 48 + 2.4 = 50.4
	if got, want := out[0].Score, 50.4; absDiff(got, want) > 0.01 {
		t.Errorf("Llanowar Elves score = %v, want %v", got, want)
	}
	if out[0].RecommendedCopies != 4 {
		t.Errorf("Llanowar Elves rec copies = %d, want 4", out[0].RecommendedCopies)
	}
	if out[0].Wildcard != "C" {
		t.Errorf("Llanowar Elves wildcard = %q, want C", out[0].Wildcard)
	}
	if len(out[0].Decks) != 2 {
		t.Errorf("Llanowar Elves should appear in 2 decks (both inclusion >= 20%%), got %d", len(out[0].Decks))
	}
}

func TestRecommendedCopies_HighShareWins(t *testing.T) {
	votes := []copyVote{
		{shareDeck: 20, avg: 4.0},
		{shareDeck: 1, avg: 1.0}, // ignored: share < 3
	}
	if got := recommendedCopies(votes); got != 4 {
		t.Errorf("recommendedCopies high-share = %d, want 4", got)
	}
}

func TestRecommendedCopies_FallbackBelowThreshold(t *testing.T) {
	votes := []copyVote{{shareDeck: 1, avg: 2.7}}
	if got := recommendedCopies(votes); got != 3 {
		t.Errorf("recommendedCopies fallback = %d, want 3", got)
	}
}

func TestRecommendedCopies_Clamps(t *testing.T) {
	if got := recommendedCopies(nil); got != 1 {
		t.Errorf("recommendedCopies(nil) = %d, want 1 (clamped from 0)", got)
	}
}

func TestIsBasicLand(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"Basic Land — Forest", true},
		{"Land — Forest", false},
		{"Creature — Elf Druid", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isBasicLand(c.in); got != c.want {
			t.Errorf("isBasicLand(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func absDiff(a, b float64) float64 {
	if a > b {
		return a - b
	}
	return b - a
}

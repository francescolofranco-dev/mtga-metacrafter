package score

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/francescolofranco-dev/mtga-metacrafter/internal/model"
	"github.com/francescolofranco-dev/mtga-metacrafter/internal/mtgtop8"
	"github.com/francescolofranco-dev/mtga-metacrafter/internal/scryfall"
)

func TestJaccard(t *testing.T) {
	a := map[string]bool{"a": true, "b": true, "c": true, "d": true}
	b := map[string]bool{"a": true, "b": true, "c": true, "e": true}
	got := jaccard(a, b)
	want := 3.0 / 5.0
	if absDiff(got, want) > 0.001 {
		t.Errorf("jaccard = %v, want %v", got, want)
	}
}

func TestCluster_MergesSimilarDecks(t *testing.T) {
	event := &mtgtop8.TournamentEvent{TierWeight: 2}
	// Two decks sharing 4 of 5 cards (Jaccard 4/6 = 0.667) — below 0.75, so
	// they should NOT cluster.
	d1 := &DeckRecord{Event: event, Archetype: "X", Cards: deckCards("A", "B", "C", "D", "E")}
	d2 := &DeckRecord{Event: event, Archetype: "X", Cards: deckCards("A", "B", "C", "D", "F")}
	clusters := clusterDecks([]*DeckRecord{d1, d2}, 0.75)
	if len(clusters) != 2 {
		t.Errorf("0.67 overlap should NOT cluster; got %d clusters", len(clusters))
	}

	// Two decks sharing 4 of 4 unique names (Jaccard 1.0) — should cluster.
	d3 := &DeckRecord{Event: event, Archetype: "X", Cards: deckCards("A", "B", "C", "D")}
	d4 := &DeckRecord{Event: event, Archetype: "X", Cards: deckCards("A", "B", "C", "D")}
	clusters = clusterDecks([]*DeckRecord{d3, d4}, 0.75)
	if len(clusters) != 1 {
		t.Errorf("identical decks should cluster; got %d clusters", len(clusters))
	}

	// Two decks sharing 6 of 7 unique names (Jaccard 6/8 = 0.75) — at the
	// threshold, should cluster.
	d5 := &DeckRecord{Event: event, Archetype: "X", Cards: deckCards("A", "B", "C", "D", "E", "F", "G")}
	d6 := &DeckRecord{Event: event, Archetype: "X", Cards: deckCards("A", "B", "C", "D", "E", "F", "H")}
	clusters = clusterDecks([]*DeckRecord{d5, d6}, 0.75)
	if len(clusters) != 1 {
		t.Errorf("0.75 overlap should cluster at threshold; got %d clusters", len(clusters))
	}
}

func TestCompute_DiversityBeatsStuffing(t *testing.T) {
	// One huge cluster of 8 identical decks all playing "Dominant" 4-of, vs
	// five small clusters of 2 decks each playing "Universal" 2-of. The
	// diverse card should outscore the stuffed one even though more decks
	// contain "Dominant".
	cards := scryfall.BuildIndex([]*scryfall.Card{
		{Name: "Dominant", Rarity: "rare", TypeLine: "Creature"},
		{Name: "Universal", Rarity: "rare", TypeLine: "Creature"},
		// filler cards to differentiate clusters (each variant has its own filler)
		{Name: "F1", Rarity: "common", TypeLine: "Creature"},
		{Name: "F2", Rarity: "common", TypeLine: "Creature"},
		{Name: "F3", Rarity: "common", TypeLine: "Creature"},
		{Name: "F4", Rarity: "common", TypeLine: "Creature"},
		{Name: "F5", Rarity: "common", TypeLine: "Creature"},
		{Name: "B1", Rarity: "common", TypeLine: "Creature"},
		{Name: "B2", Rarity: "common", TypeLine: "Creature"},
		{Name: "B3", Rarity: "common", TypeLine: "Creature"},
		{Name: "B4", Rarity: "common", TypeLine: "Creature"},
		{Name: "X1", Rarity: "common", TypeLine: "Creature"},
		{Name: "X2", Rarity: "common", TypeLine: "Creature"},
		{Name: "X3", Rarity: "common", TypeLine: "Creature"},
		{Name: "X4", Rarity: "common", TypeLine: "Creature"},
	})
	event := &mtgtop8.TournamentEvent{TierWeight: 1.5}

	var decks []*DeckRecord
	// 8 decks of one cluster: same 5 cards every time
	for i := 0; i < 8; i++ {
		decks = append(decks, &DeckRecord{
			Event: event, Archetype: "Big Deck",
			Cards: deckCards("Dominant", "B1", "B2", "B3", "B4"),
		})
	}
	// 2 decks of a small Dominant-shell variant (so Dominant qualifies under
	// MinClusterAppearancesToInclude = 2).
	for i := 0; i < 2; i++ {
		decks = append(decks, &DeckRecord{
			Event: event, Archetype: "Dom Variant",
			Cards: deckCards("Dominant", "X1", "X2", "X3", "X4"),
		})
	}
	// 5 distinct clusters of 2 decks each — each cluster has a unique filler
	// so they don't merge.
	fillers := [][]string{
		{"F1"}, {"F2"}, {"F3"}, {"F4"}, {"F5"},
	}
	for _, f := range fillers {
		for i := 0; i < 2; i++ {
			cs := append([]string{"Universal"}, f...)
			decks = append(decks, &DeckRecord{
				Event: event, Archetype: "Spread Deck", Cards: deckCards(cs...),
			})
		}
	}

	out := Compute(decks, cards, "pioneer", time.Now())
	if len(out) < 2 {
		t.Fatalf("expected ≥ 2 cards, got %d", len(out))
	}

	// Find both rankings.
	var dom, uni *model.CardRecommendation
	for _, c := range out {
		switch c.Name {
		case "Dominant":
			dom = c
		case "Universal":
			uni = c
		}
	}
	if dom == nil || uni == nil {
		t.Fatalf("missing expected cards in output")
	}
	if uni.Score <= dom.Score {
		t.Errorf("Universal (5 clusters) should outscore Dominant (1 cluster): uni=%v dom=%v", uni.Score, dom.Score)
	}
}

func TestCompute_DropsSingleClusterCard(t *testing.T) {
	cards := scryfall.BuildIndex([]*scryfall.Card{
		{Name: "Tech Card", Rarity: "rare", TypeLine: "Creature"},
	})
	event := &mtgtop8.TournamentEvent{TierWeight: 1}
	decks := []*DeckRecord{
		// One cluster (2 identical decks containing Tech Card)
		{Event: event, Archetype: "X", Cards: deckCards("Tech Card", "B1", "B2")},
		{Event: event, Archetype: "X", Cards: deckCards("Tech Card", "B1", "B2")},
	}
	out := Compute(decks, cards, "pioneer", time.Now())
	if len(out) != 0 {
		t.Errorf("single-cluster card should be dropped: got %d cards", len(out))
	}
}

func TestAnnotateCrossFormat(t *testing.T) {
	rankings := map[string]*model.FormatRanking{
		"standard": {Slug: "standard", Cards: []*model.CardRecommendation{
			{Name: "Llanowar Elves"},
			{Name: "Mossborn Hydra"},
		}},
		"pioneer": {Slug: "pioneer", Cards: []*model.CardRecommendation{
			{Name: "Llanowar Elves"},
			{Name: "Other Card"},
		}},
	}
	AnnotateCrossFormat(rankings)
	stdLlan := rankings["standard"].Cards[0]
	if len(stdLlan.AlsoIn) != 1 || stdLlan.AlsoIn[0] != "pioneer" {
		t.Errorf("Llanowar should be marked also-in pioneer; got %v", stdLlan.AlsoIn)
	}
	stdMoss := rankings["standard"].Cards[1]
	if len(stdMoss.AlsoIn) != 0 {
		t.Errorf("Mossborn should not be also-in anywhere; got %v", stdMoss.AlsoIn)
	}
}

func TestStandardRotationFactor(t *testing.T) {
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		label    string
		release  time.Time
		wantMult float64
		minDays  int
		maxDays  int
	}{
		{"fresh set, 2+ years left", time.Date(2025, 11, 1, 0, 0, 0, 0, time.UTC), 1.0, 700, 950},
		{"~181 days left", now.AddDate(0, 0, -((3*365)-181)), 1.0, 175, 190},
		{"~90 days left", now.AddDate(0, 0, -((3*365)-90)), 0.5, 85, 95},
		{"~30 days left", now.AddDate(0, 0, -((3*365)-30)), 0.2, 25, 35},
		{"~7 days left", now.AddDate(0, 0, -((3*365)-7)), 0.05, 5, 10},
		{"already rotated", now.AddDate(-4, 0, 0), 0.0, -400, -300},
	}
	for _, c := range cases {
		days, mult := standardRotationFactor(c.release, now)
		if mult != c.wantMult {
			t.Errorf("%s: multiplier = %v, want %v (days=%d)", c.label, mult, c.wantMult, days)
		}
		if days < c.minDays || days > c.maxDays {
			t.Errorf("%s: days = %d, want in [%d,%d]", c.label, days, c.minDays, c.maxDays)
		}
	}
}

func TestBuildScryfallURL_TrickyNames(t *testing.T) {
	cases := []string{
		"Sazh's Chocobo",
		"Mightform Harmonizer",
		"Surrak, Elusive Hunter",
		"Fable of the Mirror-Breaker // Reflection of Kiki-Jiki",
	}
	for _, name := range cases {
		u := buildScryfallURL(name)
		parsed, err := url.Parse(u)
		if err != nil {
			t.Errorf("buildScryfallURL(%q) not parseable: %v", name, err)
			continue
		}
		q := parsed.Query().Get("q")
		want := `!"` + name + `"`
		if q != want {
			t.Errorf("buildScryfallURL(%q) decodes to q=%q, want %q", name, q, want)
		}
		if strings.ContainsAny(u, `"' `) {
			t.Errorf("buildScryfallURL(%q) leaked unencoded chars: %s", name, u)
		}
	}
}

// deckCards is a test helper: makes a []mtgtop8.DeckCard, each with quantity 4.
func deckCards(names ...string) []mtgtop8.DeckCard {
	out := make([]mtgtop8.DeckCard, len(names))
	for i, n := range names {
		out[i] = mtgtop8.DeckCard{Name: n, Quantity: 4}
	}
	return out
}

func absDiff(a, b float64) float64 {
	if a > b {
		return a - b
	}
	return b - a
}

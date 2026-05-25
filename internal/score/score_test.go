package score

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/francescolofranco-dev/mtga-metacrafter/internal/model"
	"github.com/francescolofranco-dev/mtga-metacrafter/internal/mtggoldfish"
	"github.com/francescolofranco-dev/mtga-metacrafter/internal/scryfall"
)

func TestCompute_BasicScoring(t *testing.T) {
	cards := scryfall.BuildIndex([]*scryfall.Card{
		{Name: "Llanowar Elves", Rarity: "common", TypeLine: "Creature — Elf Druid"},
		{Name: "Mossborn Hydra", Rarity: "rare", TypeLine: "Creature — Hydra"},
		{Name: "Forest", Rarity: "common", TypeLine: "Basic Land — Forest"},
	})

	major := &mtggoldfish.TournamentEvent{Title: "Pro Tour", StarTier: 3}
	weekly := &mtggoldfish.TournamentEvent{Title: "Challenge 32", StarTier: 0}

	decks := []*DeckRecord{
		{Event: major, Archetype: "Selesnya Landfall", Cards: []mtggoldfish.DeckCard{
			{Name: "Llanowar Elves", Quantity: 4},
			{Name: "Mossborn Hydra", Quantity: 2},
			{Name: "Forest", Quantity: 7}, // skip: basic land
			{Name: "Unknown Card", Quantity: 3}, // skip: not in index
		}},
		{Event: weekly, Archetype: "Selesnya Landfall", Cards: []mtggoldfish.DeckCard{
			{Name: "Llanowar Elves", Quantity: 4},
			{Name: "Mossborn Hydra", Quantity: 4},
		}},
	}

	out := Compute(decks, cards, "standard", time.Now())
	if len(out) != 2 {
		t.Fatalf("expected 2 cards, got %d", len(out))
	}

	// 1 archetype "Selesnya Landfall" with 2 decks of tier 4 + 1.
	// avg_tier = 2.5, quality = sqrt(2 * 2.5) ≈ 2.236
	// Llanowar Elves: avg_copies=4, inclusion=1.0 → 4 × 1 × 2.236 ≈ 8.944
	if out[0].Name != "Llanowar Elves" {
		t.Errorf("expected Llanowar Elves first, got %q", out[0].Name)
	}
	if got, want := out[0].Score, 8.94; absDiff(got, want) > 0.05 {
		t.Errorf("Llanowar Elves score = %v, want ≈ %v", got, want)
	}
	if out[0].RecommendedCopies != 4 {
		t.Errorf("Llanowar Elves rec copies = %d, want 4", out[0].RecommendedCopies)
	}
	if out[0].DeckAppearances != 2 {
		t.Errorf("Llanowar Elves appearances = %d, want 2", out[0].DeckAppearances)
	}
	if out[0].Wildcard != "C" {
		t.Errorf("Llanowar Elves wildcard = %q, want C", out[0].Wildcard)
	}
	if len(out[0].Archetypes) == 0 || out[0].Archetypes[0].Name != "Selesnya Landfall" {
		t.Errorf("expected archetype 'Selesnya Landfall', got %+v", out[0].Archetypes)
	}
}

func TestCompute_PerArchetypeBeatsArchetypeStuffing(t *testing.T) {
	// The user's bug: a card stuck in one big dominant archetype shouldn't
	// always beat a card that spans multiple smaller archetypes. The new
	// per-archetype scoring should give the diverse card a real shot.
	cards := scryfall.BuildIndex([]*scryfall.Card{
		{Name: "Dominant", Rarity: "rare", TypeLine: "Creature"},   // 4-of in one big archetype
		{Name: "Universal", Rarity: "rare", TypeLine: "Creature"},  // 2-of across many archetypes
	})
	event := &mtggoldfish.TournamentEvent{Title: "Challenge", StarTier: 0}

	var decks []*DeckRecord
	// Big archetype: 8 decks, each plays 4 Dominant and nothing else relevant.
	for i := 0; i < 8; i++ {
		decks = append(decks, &DeckRecord{
			Event: event, Archetype: "Big Deck",
			Cards: []mtggoldfish.DeckCard{{Name: "Dominant", Quantity: 4}},
		})
	}
	// Five smaller archetypes, 2 decks each, each playing 2 Universal.
	for _, arch := range []string{"Arch A", "Arch B", "Arch C", "Arch D", "Arch E"} {
		for i := 0; i < 2; i++ {
			decks = append(decks, &DeckRecord{
				Event: event, Archetype: arch,
				Cards: []mtggoldfish.DeckCard{{Name: "Universal", Quantity: 2}},
			})
		}
	}

	out := Compute(decks, cards, "pioneer", time.Now())
	if len(out) != 2 {
		t.Fatalf("expected 2 cards, got %d", len(out))
	}
	// Dominant: 1 archetype, 8 decks, quality=sqrt(8*1)=2.83. contribution=4*1*2.83=11.31.
	// Universal: 5 archetypes, each 2 decks @ quality=sqrt(2*1)=1.41.
	//   per-archetype contribution = 2*1*1.41 = 2.83 → total across 5 = 14.14.
	// So Universal must rank ahead of Dominant.
	if out[0].Name != "Universal" {
		t.Errorf("Universal (5 archetypes) should outrank Dominant (1 archetype); got order: %q, %q",
			out[0].Name, out[1].Name)
	}
}

func TestCompute_DropsSingleAppearance(t *testing.T) {
	cards := scryfall.BuildIndex([]*scryfall.Card{
		{Name: "Tech Card", Rarity: "rare", TypeLine: "Creature"},
	})
	event := &mtggoldfish.TournamentEvent{Title: "x", StarTier: 1}
	decks := []*DeckRecord{
		{Event: event, Archetype: "Random", Cards: []mtggoldfish.DeckCard{
			{Name: "Tech Card", Quantity: 1},
		}},
	}
	out := Compute(decks, cards, "standard", time.Now())
	if len(out) != 0 {
		t.Errorf("expected single-deck card dropped, got %d cards", len(out))
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
		{"~3 days left", now.AddDate(0, 0, -((3*365)-3)), 0.05, 1, 5},
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

func TestCompute_StandardRotationPenalty(t *testing.T) {
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)
	// Card A: rotates in 5 days → multiplier 0.05
	rotatingSoon := now.AddDate(0, 0, -((3*365)-5))
	// Card B: rotates in 2 years → multiplier 1.0
	fresh := now.AddDate(-1, 0, 0)

	cards := scryfall.BuildIndex([]*scryfall.Card{
		{Name: "Rotating Soon", Rarity: "rare", TypeLine: "Creature", LatestRelease: rotatingSoon},
		{Name: "Fresh Card", Rarity: "rare", TypeLine: "Creature", LatestRelease: fresh},
	})
	event := &mtggoldfish.TournamentEvent{Title: "Tour", StarTier: 1}
	decks := []*DeckRecord{
		{Event: event, Archetype: "Deck1", Cards: []mtggoldfish.DeckCard{{Name: "Rotating Soon", Quantity: 4}, {Name: "Fresh Card", Quantity: 4}}},
		{Event: event, Archetype: "Deck2", Cards: []mtggoldfish.DeckCard{{Name: "Rotating Soon", Quantity: 4}, {Name: "Fresh Card", Quantity: 4}}},
	}

	stdOut := Compute(decks, cards, "standard", now)
	if len(stdOut) != 2 {
		t.Fatalf("expected 2 cards, got %d", len(stdOut))
	}
	// Find each by name
	var rot, full *model.CardRecommendation
	for _, c := range stdOut {
		if c.Name == "Rotating Soon" {
			rot = c
		}
		if c.Name == "Fresh Card" {
			full = c
		}
	}
	if rot == nil || full == nil {
		t.Fatalf("missing expected cards")
	}
	// Fresh card should be ranked first (no penalty), rotating second.
	if stdOut[0].Name != "Fresh Card" {
		t.Errorf("Fresh Card should be ranked first; got %q first", stdOut[0].Name)
	}
	if rot.DaysUntilRotation < 1 || rot.DaysUntilRotation > 10 {
		t.Errorf("Rotating Soon DaysUntilRotation = %d, expected ~5", rot.DaysUntilRotation)
	}
	if rot.Score >= rot.RawScore {
		t.Errorf("Rotating Soon final score (%v) should be < raw score (%v)", rot.Score, rot.RawScore)
	}
	// Pioneer (or any non-Standard) should ignore the penalty.
	pioOut := Compute(decks, cards, "pioneer", now)
	for _, c := range pioOut {
		if c.DaysUntilRotation != 0 {
			t.Errorf("non-standard format should not set DaysUntilRotation; got %d for %q", c.DaysUntilRotation, c.Name)
		}
		if c.Score != c.RawScore && c.RawScore != 0 {
			t.Errorf("non-standard format Score != RawScore for %q: %v vs %v", c.Name, c.Score, c.RawScore)
		}
	}
}

func TestBuildScryfallURL_TrickyNames(t *testing.T) {
	cases := []string{
		"Sazh's Chocobo",                       // apostrophe
		"Mightform Harmonizer",                 // plain
		"Surrak, Elusive Hunter",               // comma
		"Fable of the Mirror-Breaker // Reflection of Kiki-Jiki", // // split
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
		// Final URL must not contain raw ", ', commas, or // (these break Scryfall).
		if strings.ContainsAny(u, `"' `) {
			t.Errorf("buildScryfallURL(%q) leaked unencoded chars: %s", name, u)
		}
	}
}

func absDiff(a, b float64) float64 {
	if a > b {
		return a - b
	}
	return b - a
}

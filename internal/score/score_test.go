package score

import (
	"net/url"
	"strings"
	"testing"

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

	out := Compute(decks, cards)
	if len(out) != 2 {
		t.Fatalf("expected 2 cards, got %d", len(out))
	}

	// Llanowar Elves: 4*4 + 4*1 = 20
	if out[0].Name != "Llanowar Elves" {
		t.Errorf("expected Llanowar Elves first, got %q", out[0].Name)
	}
	if got, want := out[0].Score, 20.0; absDiff(got, want) > 0.01 {
		t.Errorf("Llanowar Elves score = %v, want %v", got, want)
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
	out := Compute(decks, cards)
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

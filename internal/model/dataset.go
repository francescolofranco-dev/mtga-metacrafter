package model

import "time"

// Dataset is one full snapshot covering every configured format.
// Stored in memory by the server, persisted to JSON for cold-start recovery.
type Dataset struct {
	GeneratedAt time.Time                 `json:"generated_at"`
	SourceLabel string                    `json:"source"`
	Formats     map[string]*FormatRanking `json:"formats"` // keyed by slug ("standard")
}

// FormatRanking is the per-format ranked output.
type FormatRanking struct {
	Slug        string                `json:"slug"`         // "standard"
	DisplayName string                `json:"display_name"` // "Standard"
	GeneratedAt time.Time             `json:"generated_at"`
	DeckCount   int                   `json:"deck_count"`
	Cards       []*CardRecommendation `json:"cards"`       // sorted by Score desc
	Tournaments []*TournamentRef      `json:"tournaments"` // events that contributed
}

// TournamentRef is one tournament event scraped for a format.
type TournamentRef struct {
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Date      time.Time `json:"date"`
	StarTier  int       `json:"star_tier"`  // 0-3 (MTGGoldfish star rating)
	DeckCount int       `json:"deck_count"` // decks pulled into the analysis
}

// CardRecommendation is one row on the page.
type CardRecommendation struct {
	Name              string          `json:"name"`
	Rarity            string          `json:"rarity"`   // common | uncommon | rare | mythic
	Wildcard          string          `json:"wildcard"` // C | U | R | M
	ManaCost          string          `json:"mana_cost,omitempty"`
	TypeLine          string          `json:"type_line"`
	Set               string          `json:"set"`
	ImageURI          string          `json:"image_uri"`
	ScryfallURL       string          `json:"scryfall_url"`
	Score             float64         `json:"score"`
	RecommendedCopies int             `json:"recommended_copies"` // 1-4
	DeckAppearances   int             `json:"deck_appearances"`   // total decks containing it
	Archetypes        []*ArchetypeRef `json:"archetypes"`         // archetypes featuring it
	AlsoIn            []string        `json:"also_in,omitempty"`  // other format slugs where also top-N
}

// ArchetypeRef is one archetype playing a given card.
type ArchetypeRef struct {
	Name      string  `json:"name"`
	DeckCount int     `json:"deck_count"` // decks of this archetype containing this card
	AvgCopies float64 `json:"avg_copies"`
}

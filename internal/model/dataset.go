package model

import "time"

// Dataset is the rendered output of one pipeline run. Stored in memory by the
// server and persisted to disk as JSON for cold-start recovery.
type Dataset struct {
	GeneratedAt time.Time             `json:"generated_at"`
	Format      string                `json:"format"`     // e.g. "Standard"
	SourceLabel string                `json:"source"`     // human-readable provenance
	Cards       []*CardRecommendation `json:"cards"`      // sorted by Score desc
	Archetypes  []*ArchetypeRef       `json:"archetypes"` // all archetypes seen
}

// ArchetypeRef is a top-level reference to a deck archetype.
type ArchetypeRef struct {
	Name         string  `json:"name"`
	URL          string  `json:"url"`
	MetasharePct float64 `json:"metashare_pct"`
}

// CardRecommendation is one row on the page: a card the player should
// consider crafting, ranked by total meta presence.
type CardRecommendation struct {
	Name              string     `json:"name"`
	Rarity            string     `json:"rarity"`   // common | uncommon | rare | mythic
	Wildcard          string     `json:"wildcard"` // C | U | R | M
	ManaCost          string     `json:"mana_cost"`
	TypeLine          string     `json:"type_line"`
	Set               string     `json:"set"`
	ImageURI          string     `json:"image_uri"`
	Score             float64    `json:"score"`
	RecommendedCopies int        `json:"recommended_copies"` // 1-4
	Decks             []*DeckRef `json:"decks"`
}

// DeckRef is one archetype that plays a given card.
type DeckRef struct {
	ArchetypeName string  `json:"archetype_name"`
	ArchetypeURL  string  `json:"archetype_url"`
	MetasharePct  float64 `json:"metashare_pct"`
	AvgCopies     float64 `json:"avg_copies"`
	InclusionPct  float64 `json:"inclusion_pct"`
}

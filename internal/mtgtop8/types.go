package mtgtop8

import "time"

// FormatSpec describes one format we scrape.
type FormatSpec struct {
	Slug        string // metacrafter slug used in URLs: "standard", "pioneer", …
	DisplayName string // "Standard"
	Code        string // MTGTop8 short code: "ST", "PI", "MO", …
}

// SupportedFormats enumerates the paper-Magic formats MTGTop8 covers.
// MTGA-only formats (Alchemy, Historic, Timeless) aren't here because
// MTGTop8 doesn't carry them — they'd need a separate source.
var SupportedFormats = []FormatSpec{
	{Slug: "standard", DisplayName: "Standard", Code: "ST"},
	{Slug: "pioneer", DisplayName: "Pioneer", Code: "PI"},
	{Slug: "modern", DisplayName: "Modern", Code: "MO"},
	{Slug: "pauper", DisplayName: "Pauper", Code: "PAU"},
	{Slug: "legacy", DisplayName: "Legacy", Code: "LE"},
	{Slug: "vintage", DisplayName: "Vintage", Code: "VI"},
}

// FormatBySlug returns the spec for slug, or false if unknown.
func FormatBySlug(slug string) (FormatSpec, bool) {
	for _, f := range SupportedFormats {
		if f.Slug == slug {
			return f, true
		}
	}
	return FormatSpec{}, false
}

// TournamentEvent is one event listed under a format on MTGTop8.
type TournamentEvent struct {
	Title      string
	URL        string // absolute /event?e=…&f=…
	EventID    string
	Date       time.Time
	TierWeight float64 // derived from title via keyword classification
	Standings  []DeckStanding
}

// DeckStanding is one finisher in a tournament.
type DeckStanding struct {
	Place     string // "1", "2", "3-4", etc.
	Archetype string // displayed archetype label (kept for UI only — not used in scoring)
	URL       string // absolute deck URL
	DeckID    string
}

// DeckCards is a parsed mainboard.
type DeckCards struct {
	URL   string
	Cards []DeckCard
}

// DeckCard is one mainboard card with its quantity.
type DeckCard struct {
	Name     string
	Quantity int
}

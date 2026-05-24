package mtggoldfish

import "time"

// TournamentEvent is one event listed on /tournaments/<format>.
type TournamentEvent struct {
	Title     string
	URL       string // /tournament/<id>
	Date      time.Time
	StarTier  int           // 0-3, MTGGoldfish star rating (0 = unrated)
	Standings []DeckStanding // listed finishers
}

// DeckStanding is one row in a tournament's standings table.
type DeckStanding struct {
	Place     string // "1st", "5-8", "5 - 0", etc. (raw text)
	Archetype string // archetype label (e.g. "Izzet Phoenix", "UB")
	DeckURL   string // /deck/<id>
	DeckID    string // numeric ID parsed out of the URL
}

// DeckCards is the parsed mainboard for a single deck from /deck/visual/<id>.
type DeckCards struct {
	DeckURL string
	Cards   []DeckCard // one entry per unique card with quantity
}

// DeckCard is one mainboard card with its quantity.
type DeckCard struct {
	Name     string
	Quantity int
}

// FormatSpec describes one format we scrape.
type FormatSpec struct {
	Slug        string // URL path piece: "standard", "pioneer", ...
	DisplayName string // "Standard", "Pioneer", ...
	Singleton   bool   // commander/brawl etc. — copies are always 1
}

// SupportedFormats enumerates the formats we know how to scrape.
// The slug must match MTGGoldfish's /tournaments/<slug> URL.
var SupportedFormats = []FormatSpec{
	{Slug: "standard", DisplayName: "Standard"},
	{Slug: "pioneer", DisplayName: "Pioneer"},
	{Slug: "explorer", DisplayName: "Explorer"},
	{Slug: "alchemy", DisplayName: "Alchemy"},
	{Slug: "historic", DisplayName: "Historic"},
	{Slug: "timeless", DisplayName: "Timeless"},
	{Slug: "modern", DisplayName: "Modern"},
	{Slug: "pauper", DisplayName: "Pauper"},
	{Slug: "legacy", DisplayName: "Legacy"},
	{Slug: "vintage", DisplayName: "Vintage"},
	{Slug: "commander", DisplayName: "Commander", Singleton: true},
	{Slug: "brawl", DisplayName: "Brawl", Singleton: true},
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

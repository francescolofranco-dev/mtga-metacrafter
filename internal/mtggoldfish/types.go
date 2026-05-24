package mtggoldfish

// Archetype is a single row on the Standard metagame page.
type Archetype struct {
	Name         string
	MetasharePct float64 // 0-100 scale, e.g. 11.9 for 11.9%
	URL          string  // absolute URL to the archetype/decklist page
}

// BreakdownEntry is one card listed in an archetype's "Card Breakdown" section.
// Sideboard entries are intentionally excluded by the parser.
type BreakdownEntry struct {
	CardName     string
	AvgCopies    float64 // average copies in decks of this archetype, e.g. 3.9
	InclusionPct float64 // % of decks of this archetype that play it, e.g. 98
}

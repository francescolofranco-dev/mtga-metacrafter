package scryfall

// Card holds the subset of Scryfall card metadata we render.
type Card struct {
	Name     string   `json:"name"`
	Rarity   string   `json:"rarity"`    // common | uncommon | rare | mythic
	ManaCost string   `json:"mana_cost"` // e.g. "{1}{G}{G}"
	TypeLine string   `json:"type_line"`
	Set      string   `json:"set"` // 3-letter code, e.g. "fdn"
	ImageURI string   `json:"image_uri"`
	Faces    []string `json:"faces,omitempty"` // additional face names for DFC/split/adventure
}

// Wildcard rarity letters used in MTG Arena: C / U / R / M.
func (c *Card) Wildcard() string {
	switch c.Rarity {
	case "common":
		return "C"
	case "uncommon":
		return "U"
	case "rare":
		return "R"
	case "mythic":
		return "M"
	default:
		return "?"
	}
}

package score

import (
	"math"
	"net/url"
	"sort"
	"strings"

	"github.com/francescolofranco-dev/mtga-metacrafter/internal/model"
	"github.com/francescolofranco-dev/mtga-metacrafter/internal/mtggoldfish"
	"github.com/francescolofranco-dev/mtga-metacrafter/internal/scryfall"
)

const (
	// MaxResults caps the per-format output. Long lists are noise — we hold
	// the actually-craftable shortlist.
	MaxResults = 30

	// MinAppearancesToInclude drops one-off cards that showed up in a single
	// deck — likely techs or experiments rather than crafted staples.
	MinAppearancesToInclude = 2

	// MaxArchetypesToShow caps the per-card archetype list rendered in the
	// "decks playing it" cell.
	MaxArchetypesToShow = 5
)

// DeckRecord ties a parsed decklist to its tournament context.
type DeckRecord struct {
	Event     *mtggoldfish.TournamentEvent
	Archetype string
	Cards     []mtggoldfish.DeckCard
}

// Compute ranks cards across all submitted decks.
//
// Scoring:
//
//	weight(event) = event.StarTier + 1            // 0 stars → 1, 3 stars → 4
//	score(card)   = Σ_deck (copies × weight)
//
// Recommended copies = highest copy count seen for that card in any single
// deck, clamped to 1–4.
func Compute(decks []*DeckRecord, cards *scryfall.Index) []*model.CardRecommendation {
	type agg struct {
		card            *scryfall.Card
		score           float64
		appearances     int
		maxCopies       int
		archetypeCount  map[string]int
		archetypeCopies map[string]int
	}
	aggs := map[string]*agg{}

	for _, rec := range decks {
		w := tierWeight(rec.Event)
		for _, dc := range rec.Cards {
			card, ok := cards.Lookup(dc.Name)
			if !ok {
				continue
			}
			if isBasicLand(card.TypeLine) {
				continue
			}
			key := strings.ToLower(card.Name)
			cur := aggs[key]
			if cur == nil {
				cur = &agg{
					card:            card,
					archetypeCount:  map[string]int{},
					archetypeCopies: map[string]int{},
				}
				aggs[key] = cur
			}
			cur.score += float64(dc.Quantity) * float64(w)
			cur.appearances++
			if dc.Quantity > cur.maxCopies {
				cur.maxCopies = dc.Quantity
			}
			cur.archetypeCount[rec.Archetype]++
			cur.archetypeCopies[rec.Archetype] += dc.Quantity
		}
	}

	out := make([]*model.CardRecommendation, 0, len(aggs))
	for _, a := range aggs {
		if a.appearances < MinAppearancesToInclude {
			continue
		}
		out = append(out, &model.CardRecommendation{
			Name:              a.card.Name,
			Rarity:            a.card.Rarity,
			Wildcard:          a.card.Wildcard(),
			ManaCost:          a.card.ManaCost,
			TypeLine:          a.card.TypeLine,
			Set:               a.card.Set,
			ImageURI:          a.card.ImageURI,
			ScryfallURL:       buildScryfallURL(a.card.Name),
			Score:             round2(a.score),
			RecommendedCopies: clampCopies(a.maxCopies),
			DeckAppearances:   a.appearances,
			Archetypes:        topArchetypes(a.archetypeCount, a.archetypeCopies),
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > MaxResults {
		out = out[:MaxResults]
	}
	return out
}

// AnnotateCrossFormat sets CardRecommendation.AlsoIn on every card based on
// whether the same card name also appears in the top-N of other formats.
func AnnotateCrossFormat(rankings map[string]*model.FormatRanking) {
	formatCards := map[string]map[string]bool{}
	for slug, r := range rankings {
		set := make(map[string]bool, len(r.Cards))
		for _, c := range r.Cards {
			set[strings.ToLower(c.Name)] = true
		}
		formatCards[slug] = set
	}

	for slug, r := range rankings {
		for _, c := range r.Cards {
			key := strings.ToLower(c.Name)
			var also []string
			for otherSlug, set := range formatCards {
				if otherSlug == slug {
					continue
				}
				if set[key] {
					also = append(also, otherSlug)
				}
			}
			sort.Strings(also)
			c.AlsoIn = also
		}
	}
}

func tierWeight(e *mtggoldfish.TournamentEvent) int {
	if e == nil {
		return 1
	}
	return e.StarTier + 1
}

func isBasicLand(typeLine string) bool {
	low := strings.ToLower(typeLine)
	return strings.Contains(low, "basic") && strings.Contains(low, "land")
}

func clampCopies(n int) int {
	switch {
	case n < 1:
		return 1
	case n > 4:
		return 4
	default:
		return n
	}
}

func round2(f float64) float64 {
	return math.Round(f*100) / 100
}

func topArchetypes(counts, copies map[string]int) []*model.ArchetypeRef {
	refs := make([]*model.ArchetypeRef, 0, len(counts))
	for name, c := range counts {
		cp := copies[name]
		avg := 0.0
		if c > 0 {
			avg = float64(cp) / float64(c)
		}
		refs = append(refs, &model.ArchetypeRef{
			Name:      name,
			DeckCount: c,
			AvgCopies: math.Round(avg*10) / 10,
		})
	}
	sort.SliceStable(refs, func(i, j int) bool {
		if refs[i].DeckCount != refs[j].DeckCount {
			return refs[i].DeckCount > refs[j].DeckCount
		}
		return refs[i].Name < refs[j].Name
	})
	if len(refs) > MaxArchetypesToShow {
		refs = refs[:MaxArchetypesToShow]
	}
	return refs
}

// buildScryfallURL returns a Scryfall search URL that lands on the exact card.
// We URL-encode the whole `!"<name>"` query so quotes, commas, apostrophes,
// and the "//" in split-card names round-trip correctly.
func buildScryfallURL(name string) string {
	q := `!"` + name + `"`
	return "https://scryfall.com/search?q=" + url.QueryEscape(q)
}

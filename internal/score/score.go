package score

import (
	"math"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/francescolofranco-dev/mtga-metacrafter/internal/model"
	"github.com/francescolofranco-dev/mtga-metacrafter/internal/mtggoldfish"
	"github.com/francescolofranco-dev/mtga-metacrafter/internal/scryfall"
)

// StandardSetLifespan approximates how long a set stays Standard-legal after
// release. Used to estimate rotation distance for the Standard-only penalty.
const StandardSetLifespan = 3 * 365 * 24 * time.Hour

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

// Compute ranks cards using per-archetype aggregation.
//
// For each archetype A we compute:
//
//	quality(A) = sqrt( decks_in_A × avg_tier_weight_in_A )
//
// and for each card C in A:
//
//	avg_copies(C,A)    = mean copies in decks-of-A that contain C
//	inclusion(C,A)     = fraction of decks of A that contain C
//	contribution(C,A)  = avg_copies × inclusion × quality(A)
//
// The card's score is the sum of its contributions across all archetypes it
// appears in:
//
//	raw_score(C) = Σ_A contribution(C, A)
//
// Aggregating per-archetype (not per-deck) is the key change: it prevents a
// single dominant archetype from monopolizing the top of the list, and it
// rewards cards that span multiple archetypes — exactly the "if I craft this,
// how many decks does it unlock?" question a player is asking.
//
// Tier weights (per event):
//
//	3-star Pro Tour       → 4
//	2-star regional       → 3
//	1-star notable        → 2
//	unrated paper / MTGO  → 1
//	MTGO 5-0 league       → 0.5
//
// For format "standard", the raw score is multiplied by a rotation-distance
// penalty (cards close to leaving Standard are sharply discounted).
// Recommended copies = highest copy count seen for the card in any single deck.
func Compute(decks []*DeckRecord, cards *scryfall.Index, formatSlug string, now time.Time) []*model.CardRecommendation {
	// Group decks by archetype.
	byArch := map[string][]*DeckRecord{}
	for _, d := range decks {
		byArch[d.Archetype] = append(byArch[d.Archetype], d)
	}

	type agg struct {
		card             *scryfall.Card
		score            float64
		totalAppearances int // across all archetypes
		maxCopies        int
		archetypes       map[string]*model.ArchetypeRef // archetype name -> ref
	}
	aggs := map[string]*agg{}

	for archName, archDecks := range byArch {
		// archetype quality
		tierSum := 0.0
		for _, d := range archDecks {
			tierSum += tierWeight(d.Event)
		}
		avgTier := tierSum / float64(len(archDecks))
		quality := math.Sqrt(float64(len(archDecks)) * avgTier)

		// for each card seen in this archetype, sum copies and count decks
		type counts struct {
			deckCount  int
			copiesSum  int
			maxCopies  int
			scryCard   *scryfall.Card
		}
		archCards := map[string]*counts{}

		for _, d := range archDecks {
			seenInDeck := map[string]bool{}
			for _, dc := range d.Cards {
				card, ok := cards.Lookup(dc.Name)
				if !ok {
					continue
				}
				if isBasicLand(card.TypeLine) {
					continue
				}
				key := strings.ToLower(card.Name)
				if seenInDeck[key] {
					continue // protect against duplicate face matches
				}
				seenInDeck[key] = true
				c := archCards[key]
				if c == nil {
					c = &counts{scryCard: card}
					archCards[key] = c
				}
				c.deckCount++
				c.copiesSum += dc.Quantity
				if dc.Quantity > c.maxCopies {
					c.maxCopies = dc.Quantity
				}
			}
		}

		// fold per-archetype contributions into the global aggregates
		for key, c := range archCards {
			avgCopies := float64(c.copiesSum) / float64(c.deckCount)
			inclusion := float64(c.deckCount) / float64(len(archDecks))
			contribution := avgCopies * inclusion * quality

			cur := aggs[key]
			if cur == nil {
				cur = &agg{
					card:       c.scryCard,
					archetypes: map[string]*model.ArchetypeRef{},
				}
				aggs[key] = cur
			}
			cur.score += contribution
			cur.totalAppearances += c.deckCount
			if c.maxCopies > cur.maxCopies {
				cur.maxCopies = c.maxCopies
			}
			cur.archetypes[archName] = &model.ArchetypeRef{
				Name:      archName,
				DeckCount: c.deckCount,
				AvgCopies: math.Round(avgCopies*10) / 10,
			}
		}
	}

	out := make([]*model.CardRecommendation, 0, len(aggs))
	for _, a := range aggs {
		if a.totalAppearances < MinAppearancesToInclude {
			continue
		}
		raw := a.score
		final := raw
		var daysLeft int
		if formatSlug == "standard" && !a.card.LatestRelease.IsZero() {
			days, mult := standardRotationFactor(a.card.LatestRelease, now)
			daysLeft = days
			final = raw * mult
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
			Score:             round2(final),
			RawScore:          round2(raw),
			RecommendedCopies: clampCopies(a.maxCopies),
			DeckAppearances:   a.totalAppearances,
			Archetypes:        flattenArchetypes(a.archetypes),
			DaysUntilRotation: daysLeft,
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

// flattenArchetypes turns the map into the display-sorted slice (most
// decks first, ties alphabetic) and caps to MaxArchetypesToShow.
func flattenArchetypes(m map[string]*model.ArchetypeRef) []*model.ArchetypeRef {
	out := make([]*model.ArchetypeRef, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].DeckCount != out[j].DeckCount {
			return out[i].DeckCount > out[j].DeckCount
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > MaxArchetypesToShow {
		out = out[:MaxArchetypesToShow]
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

func tierWeight(e *mtggoldfish.TournamentEvent) float64 {
	if e == nil {
		return 1
	}
	if e.IsLeague {
		return 0.5
	}
	return float64(e.StarTier + 1)
}

// standardRotationFactor returns (days_until_rotation, multiplier).
// The multiplier is 1.0 for cards with > 180 days left and drops sharply
// the closer rotation gets. Cards estimated to be past rotation get 0.
func standardRotationFactor(latestRelease, now time.Time) (int, float64) {
	rotation := latestRelease.Add(StandardSetLifespan)
	d := rotation.Sub(now)
	days := int(d.Hours() / 24)
	switch {
	case days <= 0:
		return days, 0.0
	case days <= 7:
		return days, 0.05
	case days <= 30:
		return days, 0.2
	case days <= 90:
		return days, 0.5
	case days <= 180:
		return days, 0.8
	default:
		return days, 1.0
	}
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

// buildScryfallURL returns a Scryfall search URL that lands on the exact card.
// We URL-encode the whole `!"<name>"` query so quotes, commas, apostrophes,
// and the "//" in split-card names round-trip correctly.
func buildScryfallURL(name string) string {
	q := `!"` + name + `"`
	return "https://scryfall.com/search?q=" + url.QueryEscape(q)
}

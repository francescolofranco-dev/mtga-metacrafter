package score

import (
	"math"
	"sort"
	"strings"

	"github.com/francescolofranco-dev/mtga-metacrafter/internal/model"
	"github.com/francescolofranco-dev/mtga-metacrafter/internal/mtggoldfish"
	"github.com/francescolofranco-dev/mtga-metacrafter/internal/scryfall"
)

// MinArchetypeShareForCopies is the metashare threshold below which an
// archetype's votes don't influence "recommended copies". A 1-of in a 2%
// rogue deck shouldn't pull a 4-of-in-an-18%-deck recommendation down.
const MinArchetypeShareForCopies = 3.0

// MinInclusionForDeckList is the per-archetype inclusion threshold for
// listing a card under that archetype's "decks playing it" section. Cards
// played in <20% of an archetype's lists are too peripheral to feature.
const MinInclusionForDeckList = 20.0

// MaxResults caps the output to the top N cards by score.
const MaxResults = 100

// Compute builds the ranked recommendation list.
//
// Scoring (each archetype contributes additively):
//
//	score(c) = Σ_archetype (metashare_pct × inclusion_pct × avg_copies / 100)
//
// Both percentages are on a 0–100 scale. A 4-of with 100% inclusion in a 20%
// meta deck contributes 20 × 100 × 4 / 100 = 80 points. A 1-of with 30%
// inclusion in a 5% deck contributes 5 × 30 × 1 / 100 = 1.5 points.
//
// breakdowns is keyed by archetype URL.
func Compute(
	archetypes []mtggoldfish.Archetype,
	breakdowns map[string][]mtggoldfish.BreakdownEntry,
	cards *scryfall.Index,
) []*model.CardRecommendation {

	type agg struct {
		card  *scryfall.Card
		score float64
		votes []copyVote
		decks []*model.DeckRef
	}
	aggs := map[string]*agg{}

	for _, a := range archetypes {
		for _, e := range breakdowns[a.URL] {
			card, ok := cards.Lookup(e.CardName)
			if !ok {
				continue // typos, non-Standard, etc.
			}
			if isBasicLand(card.TypeLine) {
				continue // basic lands are free in MTGA
			}

			key := strings.ToLower(card.Name)
			cur := aggs[key]
			if cur == nil {
				cur = &agg{card: card}
				aggs[key] = cur
			}

			cur.score += a.MetasharePct * e.InclusionPct * e.AvgCopies / 100
			cur.votes = append(cur.votes, copyVote{shareDeck: a.MetasharePct, avg: e.AvgCopies})

			if e.InclusionPct >= MinInclusionForDeckList {
				cur.decks = append(cur.decks, &model.DeckRef{
					ArchetypeName: a.Name,
					ArchetypeURL:  a.URL,
					MetasharePct:  a.MetasharePct,
					AvgCopies:     e.AvgCopies,
					InclusionPct:  e.InclusionPct,
				})
			}
		}
	}

	out := make([]*model.CardRecommendation, 0, len(aggs))
	for _, a := range aggs {
		sort.SliceStable(a.decks, func(i, j int) bool {
			return a.decks[i].MetasharePct > a.decks[j].MetasharePct
		})
		out = append(out, &model.CardRecommendation{
			Name:              a.card.Name,
			Rarity:            a.card.Rarity,
			Wildcard:          a.card.Wildcard(),
			ManaCost:          a.card.ManaCost,
			TypeLine:          a.card.TypeLine,
			Set:               a.card.Set,
			ImageURI:          a.card.ImageURI,
			Score:             round2(a.score),
			RecommendedCopies: recommendedCopies(a.votes),
			Decks:             a.decks,
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

type copyVote struct {
	shareDeck float64 // metashare of the archetype
	avg       float64 // avg copies in that archetype
}

func recommendedCopies(votes []copyVote) int {
	max := 0.0
	found := false
	for _, v := range votes {
		if v.shareDeck < MinArchetypeShareForCopies {
			continue
		}
		if v.avg > max {
			max = v.avg
			found = true
		}
	}
	if !found {
		for _, v := range votes {
			if v.avg > max {
				max = v.avg
			}
		}
	}
	r := int(math.Round(max))
	if r < 1 {
		r = 1
	} else if r > 4 {
		r = 4
	}
	return r
}

func isBasicLand(typeLine string) bool {
	low := strings.ToLower(typeLine)
	return strings.Contains(low, "basic") && strings.Contains(low, "land")
}

func round2(f float64) float64 {
	return math.Round(f*100) / 100
}

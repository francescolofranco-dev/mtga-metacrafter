package score

import (
	"math"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/francescolofranco-dev/mtga-metacrafter/internal/model"
	"github.com/francescolofranco-dev/mtga-metacrafter/internal/mtgtop8"
	"github.com/francescolofranco-dev/mtga-metacrafter/internal/scryfall"
)

// StandardSetLifespan approximates how long a set stays Standard-legal after
// release. Used to estimate rotation distance for the Standard-only penalty.
const StandardSetLifespan = 3 * 365 * 24 * time.Hour

const (
	// MaxResults caps the per-format output.
	MaxResults = 30

	// MinClusterAppearancesToInclude drops cards that show up in only one
	// distinct deck cluster — single-list tech, not worth crafting yet.
	MinClusterAppearancesToInclude = 2

	// MaxArchetypesToShow caps the per-card cluster list rendered in the
	// "decks playing it" cell.
	MaxArchetypesToShow = 5

	// JaccardThreshold is how much overlap on unique mainboard card names is
	// needed for two decks to count as the same cluster. 0.75 means 75% of
	// the unique names must be shared.
	JaccardThreshold = 0.75
)

// DeckRecord ties a parsed mainboard to its tournament context.
type DeckRecord struct {
	Event     *mtgtop8.TournamentEvent
	Archetype string // display label from the source (NOT used for grouping)
	Cards     []mtgtop8.DeckCard
}

// Compute clusters decks by mainboard similarity and ranks cards by their
// presence across distinct clusters.
//
// Scoring (no archetype label is used):
//
//  1. Group decks into clusters where every pair has Jaccard similarity ≥ 0.75
//     on the set of unique mainboard card names. Greedy first-match assignment.
//  2. For each cluster, compute:
//       avg_event_tier  =  mean event TierWeight across the cluster's decks
//       size_weight     =  sqrt(num_decks_in_cluster)
//  3. For each card C in a cluster:
//       avg_copies(C)   =  mean copies in cluster decks that contain C
//       contribution(C, cluster) = avg_copies × avg_event_tier × size_weight
//     score(C) = Σ contribution(C, cluster)  over every cluster containing C
//
// A card spanning many distinct clusters can outscore a card stuck in one big
// cluster — which is the point: more crafting flexibility = more value.
//
// For format "standard" we then multiply by a rotation-distance penalty
// (cards close to rotating out are sharply discounted).
func Compute(decks []*DeckRecord, cards *scryfall.Index, formatSlug string, now time.Time) []*model.CardRecommendation {
	clusters := clusterDecks(decks, JaccardThreshold)

	type agg struct {
		card             *scryfall.Card
		score            float64
		clusterCount     int
		maxCopies        int
		// archetype-label-by-cluster, for display only
		clusterRefs map[*deckCluster]*model.ArchetypeRef
	}
	aggs := map[string]*agg{}

	for _, cl := range clusters {
		avgTier := cl.avgTier()
		sizeWeight := math.Sqrt(float64(len(cl.decks)))
		clusterLabel := cl.dominantLabel()

		for cardName, info := range cl.cardInfo(cards) {
			avgCopies := float64(info.totalCopies) / float64(info.deckCount)
			contribution := avgCopies * avgTier * sizeWeight

			key := strings.ToLower(cardName)
			cur := aggs[key]
			if cur == nil {
				cur = &agg{
					card:        info.card,
					clusterRefs: map[*deckCluster]*model.ArchetypeRef{},
				}
				aggs[key] = cur
			}
			cur.score += contribution
			cur.clusterCount++
			if info.maxCopies > cur.maxCopies {
				cur.maxCopies = info.maxCopies
			}
			cur.clusterRefs[cl] = &model.ArchetypeRef{
				Name:      clusterLabel,
				DeckCount: len(cl.decks),
				AvgCopies: math.Round(avgCopies*10) / 10,
			}
		}
	}

	out := make([]*model.CardRecommendation, 0, len(aggs))
	for _, a := range aggs {
		if a.clusterCount < MinClusterAppearancesToInclude {
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
			DeckAppearances:   totalDecks(a.clusterRefs),
			Archetypes:        flattenClusters(a.clusterRefs),
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

func totalDecks(refs map[*deckCluster]*model.ArchetypeRef) int {
	t := 0
	for _, r := range refs {
		t += r.DeckCount
	}
	return t
}

func flattenClusters(refs map[*deckCluster]*model.ArchetypeRef) []*model.ArchetypeRef {
	out := make([]*model.ArchetypeRef, 0, len(refs))
	for _, r := range refs {
		out = append(out, r)
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

// standardRotationFactor returns (days_until_rotation, multiplier).
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
func buildScryfallURL(name string) string {
	q := `!"` + name + `"`
	return "https://scryfall.com/search?q=" + url.QueryEscape(q)
}

package score

import (
	"sort"
	"strings"

	"github.com/francescolofranco-dev/mtga-metacrafter/internal/mtgtop8"
	"github.com/francescolofranco-dev/mtga-metacrafter/internal/scryfall"
)

// deckCluster is a group of decks whose mainboards are pairwise above the
// Jaccard similarity threshold (loosely — we use a greedy assignment, so the
// guarantee is only "≥ threshold to the cluster representative seen so far").
type deckCluster struct {
	// nameSet is the representative set of unique mainboard card names
	// (lowercased), built from the first deck assigned. We compare against
	// this when deciding membership.
	nameSet map[string]bool
	decks   []*DeckRecord
	// archetypeLabels is the bag of source-provided archetype labels across
	// the cluster's decks. The most common one is used for display.
	archetypeLabels map[string]int
}

func newCluster(d *DeckRecord) *deckCluster {
	return &deckCluster{
		nameSet:         uniqueNames(d.Cards),
		decks:           []*DeckRecord{d},
		archetypeLabels: map[string]int{d.Archetype: 1},
	}
}

func (c *deckCluster) accept(d *DeckRecord) {
	c.decks = append(c.decks, d)
	if d.Archetype != "" {
		c.archetypeLabels[d.Archetype]++
	}
	// Optional: union the new deck's cards into the cluster's representative
	// name set. This keeps the cluster from "drifting" too far from the
	// original list while still tolerating local swaps. We skip this for now;
	// the first-deck representative is sufficient for the user's intent.
}

func (c *deckCluster) avgTier() float64 {
	if len(c.decks) == 0 {
		return 1
	}
	sum := 0.0
	for _, d := range c.decks {
		if d.Event != nil {
			sum += d.Event.TierWeight
		} else {
			sum += 1
		}
	}
	return sum / float64(len(c.decks))
}

func (c *deckCluster) dominantLabel() string {
	best := ""
	bestN := 0
	for name, n := range c.archetypeLabels {
		if n > bestN || (n == bestN && name < best) {
			best = name
			bestN = n
		}
	}
	if best == "" {
		return "Deck"
	}
	return best
}

// cardCount captures per-card stats inside one cluster.
type cardCount struct {
	card        *scryfall.Card
	deckCount   int
	totalCopies int
	maxCopies   int
}

// cardInfo aggregates per-card stats over the cluster's decks. Cards not in
// the Scryfall index, and basic lands, are dropped (basic lands aren't
// craftable in MTGA).
func (c *deckCluster) cardInfo(cards *scryfall.Index) map[string]*cardCount {
	out := map[string]*cardCount{}
	for _, d := range c.decks {
		seenInDeck := map[string]bool{}
		for _, dc := range d.Cards {
			card, ok := cards.Lookup(dc.Name)
			if !ok || isBasicLand(card.TypeLine) {
				continue
			}
			key := card.Name
			if seenInDeck[key] {
				continue // protect against duplicate face-name matches
			}
			seenInDeck[key] = true
			cur := out[key]
			if cur == nil {
				cur = &cardCount{card: card}
				out[key] = cur
			}
			cur.deckCount++
			cur.totalCopies += dc.Quantity
			if dc.Quantity > cur.maxCopies {
				cur.maxCopies = dc.Quantity
			}
		}
	}
	return out
}

// clusterDecks greedily assigns each deck to the first cluster whose
// representative card-name set is within the Jaccard threshold; otherwise it
// starts a new cluster. To keep ranking deterministic across runs we process
// decks in tier-then-arrival order.
func clusterDecks(decks []*DeckRecord, threshold float64) []*deckCluster {
	sorted := make([]*DeckRecord, len(decks))
	copy(sorted, decks)
	sort.SliceStable(sorted, func(i, j int) bool {
		ti := tierOf(sorted[i])
		tj := tierOf(sorted[j])
		if ti != tj {
			return ti > tj
		}
		return false
	})

	var clusters []*deckCluster
	for _, d := range sorted {
		names := uniqueNames(d.Cards)
		if len(names) == 0 {
			continue
		}
		bestIdx := -1
		bestSim := threshold
		for i, c := range clusters {
			s := jaccard(names, c.nameSet)
			if s >= bestSim {
				bestSim = s
				bestIdx = i
			}
		}
		if bestIdx >= 0 {
			clusters[bestIdx].accept(d)
		} else {
			clusters = append(clusters, newCluster(d))
		}
	}
	return clusters
}

func tierOf(d *DeckRecord) float64 {
	if d == nil || d.Event == nil {
		return 0
	}
	return d.Event.TierWeight
}

// uniqueNames returns the set of unique mainboard card names (lowercased)
// for a deck. Basic lands stay in — they affect color identity comparison.
func uniqueNames(cards []mtgtop8.DeckCard) map[string]bool {
	out := make(map[string]bool, len(cards))
	for _, c := range cards {
		name := strings.ToLower(strings.TrimSpace(c.Name))
		if name == "" {
			continue
		}
		out[name] = true
	}
	return out
}

// jaccard computes |A ∩ B| / |A ∪ B| for two name sets.
func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	inter := 0
	for name := range a {
		if b[name] {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func isBasicLand(typeLine string) bool {
	low := strings.ToLower(typeLine)
	return strings.Contains(low, "basic") && strings.Contains(low, "land")
}

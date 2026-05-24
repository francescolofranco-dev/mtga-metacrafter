package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/francescolofranco-dev/mtga-metacrafter/internal/model"
	"github.com/francescolofranco-dev/mtga-metacrafter/internal/mtggoldfish"
	"github.com/francescolofranco-dev/mtga-metacrafter/internal/score"
	"github.com/francescolofranco-dev/mtga-metacrafter/internal/scryfall"
)

// ErrScrapeDegraded is returned when the scraped data fails sanity checks.
// Callers should keep the previous dataset and try again later.
var ErrScrapeDegraded = errors.New("pipeline: scrape result failed sanity checks")

// Config controls a single pipeline run.
type Config struct {
	Scryfall    *scryfall.Client
	MTGGoldfish *mtggoldfish.Client
	Logger      *slog.Logger
	MaxArchetypes int // 0 means "all"
}

// Run fetches all sources, computes the recommendation list, and returns a
// Dataset. If the result fails sanity checks, returns ErrScrapeDegraded.
func Run(ctx context.Context, cfg Config) (*model.Dataset, error) {
	logger := cfg.Logger.With("op", "pipeline.run")
	t0 := time.Now()

	logger.Info("fetching Scryfall Standard-legal cards")
	cards, err := cfg.Scryfall.FetchStandardLegal(ctx)
	if err != nil {
		return nil, fmt.Errorf("scryfall: %w", err)
	}
	index := scryfall.BuildIndex(cards)
	logger.Info("scryfall index built", "cards", len(cards), "index_keys", index.Len())

	logger.Info("fetching MTGGoldfish meta")
	archs, err := cfg.MTGGoldfish.FetchMeta(ctx)
	if err != nil {
		return nil, fmt.Errorf("mtggoldfish meta: %w", err)
	}
	logger.Info("metagame parsed", "archetypes", len(archs))

	if cfg.MaxArchetypes > 0 && len(archs) > cfg.MaxArchetypes {
		archs = archs[:cfg.MaxArchetypes]
	}

	breakdowns := make(map[string][]mtggoldfish.BreakdownEntry, len(archs))
	missCount := 0
	for _, a := range archs {
		entries, err := cfg.MTGGoldfish.FetchArchetype(ctx, a.URL)
		if err != nil {
			logger.Warn("archetype fetch failed", "archetype", a.Name, "err", err)
			missCount++
			continue
		}
		breakdowns[a.URL] = entries
		logger.Debug("archetype parsed", "archetype", a.Name, "entries", len(entries))
	}

	cardRecs := score.Compute(archs, breakdowns, index)

	ds := &model.Dataset{
		GeneratedAt: time.Now().UTC(),
		Format:      "Standard",
		SourceLabel: "MTGGoldfish + Scryfall",
		Cards:       cardRecs,
		Archetypes:  toArchetypeRefs(archs),
	}

	if err := sanityCheck(ds, missCount, len(archs)); err != nil {
		logger.Warn("scrape degraded — rejecting result",
			"archetypes", len(archs),
			"missed_archetype_fetches", missCount,
			"cards", len(cardRecs),
			"elapsed", time.Since(t0).String(),
			"err", err)
		return ds, fmt.Errorf("%w: %v", ErrScrapeDegraded, err)
	}

	logger.Info("pipeline complete",
		"archetypes", len(archs),
		"cards", len(cardRecs),
		"elapsed", time.Since(t0).String())
	return ds, nil
}

func sanityCheck(ds *model.Dataset, archetypeMisses, totalArchetypes int) error {
	if len(ds.Archetypes) < 5 {
		return fmt.Errorf("only %d archetypes found", len(ds.Archetypes))
	}
	var sumShare float64
	for _, a := range ds.Archetypes {
		sumShare += a.MetasharePct
	}
	if sumShare < 50 || sumShare > 150 {
		return fmt.Errorf("metashare sum %.1f%% outside [50,150]", sumShare)
	}
	if len(ds.Cards) < 30 {
		return fmt.Errorf("only %d card recommendations produced", len(ds.Cards))
	}
	// More than 30% of archetype-detail fetches failing makes the data unreliable.
	if totalArchetypes > 0 && float64(archetypeMisses)/float64(totalArchetypes) > 0.3 {
		return fmt.Errorf("too many archetype fetch failures: %d/%d", archetypeMisses, totalArchetypes)
	}
	return nil
}

func toArchetypeRefs(archs []mtggoldfish.Archetype) []*model.ArchetypeRef {
	out := make([]*model.ArchetypeRef, 0, len(archs))
	for _, a := range archs {
		out = append(out, &model.ArchetypeRef{
			Name:         a.Name,
			URL:          a.URL,
			MetasharePct: a.MetasharePct,
		})
	}
	return out
}

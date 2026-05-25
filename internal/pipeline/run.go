package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/francescolofranco-dev/mtga-metacrafter/internal/model"
	"github.com/francescolofranco-dev/mtga-metacrafter/internal/mtgtop8"
	"github.com/francescolofranco-dev/mtga-metacrafter/internal/score"
	"github.com/francescolofranco-dev/mtga-metacrafter/internal/scryfall"
)

// ErrScrapeDegraded is returned when the scraped data fails sanity checks.
// Callers should keep the previous dataset and try again later.
var ErrScrapeDegraded = errors.New("pipeline: scrape result failed sanity checks")

// Config controls one pipeline run.
type Config struct {
	Scryfall *scryfall.Client
	MTGTop8  *mtgtop8.Client
	Logger   *slog.Logger

	// Formats to scrape (must be one of mtgtop8.SupportedFormats).
	Formats []mtgtop8.FormatSpec

	// MaxEventsPerFormat caps the most-recent events per format. Default 8.
	MaxEventsPerFormat int

	// MaxDecksPerEvent caps deck fetches per event (top of standings). Default 8.
	MaxDecksPerEvent int

	// MaxEventAgeDays drops events older than this. Default 60.
	MaxEventAgeDays int
}

const (
	defaultMaxEvents    = 8
	defaultMaxDecks     = 8
	defaultMaxAgeDays   = 60
	minFormatsToSucceed = 1
)

// Run fetches every configured format, ranks each one (per-deck-cluster
// scoring), and joins them into one Dataset.
func Run(ctx context.Context, cfg Config) (*model.Dataset, error) {
	cfg = cfg.withDefaults()
	logger := cfg.Logger.With("op", "pipeline.run")
	t0 := time.Now()

	if len(cfg.Formats) == 0 {
		return nil, fmt.Errorf("no formats configured")
	}

	formatSlugs := make([]string, 0, len(cfg.Formats))
	for _, f := range cfg.Formats {
		formatSlugs = append(formatSlugs, f.Slug)
	}

	logger.Info("fetching Scryfall card data", "formats", formatSlugs)
	cards, err := cfg.Scryfall.FetchLegalInAny(ctx, formatSlugs)
	if err != nil {
		return nil, fmt.Errorf("scryfall: %w", err)
	}
	index := scryfall.BuildIndex(cards)
	logger.Info("scryfall index built", "cards", len(cards), "index_keys", index.Len())

	rankings := map[string]*model.FormatRanking{}
	totalEvents := 0
	totalDecks := 0
	cutoff := time.Now().UTC().AddDate(0, 0, -cfg.MaxEventAgeDays)

	for _, f := range cfg.Formats {
		flog := logger.With("format", f.Slug)
		fr, err := runFormat(ctx, cfg, index, f, cutoff, flog)
		if err != nil {
			flog.Warn("format scrape failed — skipping", "err", err)
			continue
		}
		rankings[f.Slug] = fr
		totalEvents += len(fr.Tournaments)
		totalDecks += fr.DeckCount
	}

	score.AnnotateCrossFormat(rankings)

	ds := &model.Dataset{
		GeneratedAt: time.Now().UTC(),
		SourceLabel: "MTGTop8 (Large Events) + Scryfall",
		Formats:     rankings,
	}

	if err := sanityCheck(ds); err != nil {
		logger.Warn("scrape degraded — rejecting result",
			"formats_seen", len(rankings),
			"total_events", totalEvents,
			"total_decks", totalDecks,
			"elapsed", time.Since(t0).String(),
			"err", err)
		return ds, fmt.Errorf("%w: %v", ErrScrapeDegraded, err)
	}

	logger.Info("pipeline complete",
		"formats", len(rankings),
		"events", totalEvents,
		"decks", totalDecks,
		"elapsed", time.Since(t0).String())
	return ds, nil
}

func runFormat(
	ctx context.Context,
	cfg Config,
	index *scryfall.Index,
	f mtgtop8.FormatSpec,
	cutoff time.Time,
	logger *slog.Logger,
) (*model.FormatRanking, error) {
	logger.Info("fetching format index")
	events, err := cfg.MTGTop8.FetchFormatIndex(ctx, f)
	if err != nil {
		return nil, fmt.Errorf("format index: %w", err)
	}

	filtered := events[:0]
	for _, e := range events {
		if !e.Date.IsZero() && e.Date.Before(cutoff) {
			continue
		}
		filtered = append(filtered, e)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return filtered[i].Date.After(filtered[j].Date)
	})
	if len(filtered) > cfg.MaxEventsPerFormat {
		filtered = filtered[:cfg.MaxEventsPerFormat]
	}
	logger.Info("format index filtered", "kept", len(filtered), "raw", len(events))

	type planEntry struct {
		event    *mtgtop8.TournamentEvent
		standing mtgtop8.DeckStanding
	}
	seen := map[string]bool{}
	var plan []planEntry
	for i := range filtered {
		ev := &filtered[i]
		standings, err := cfg.MTGTop8.FetchEvent(ctx, ev.URL)
		if err != nil || len(standings) == 0 {
			if err != nil {
				logger.Warn("event detail fetch failed", "event", ev.Title, "err", err)
			}
			continue
		}
		ev.Standings = standings
		limit := cfg.MaxDecksPerEvent
		if limit > len(standings) {
			limit = len(standings)
		}
		for _, s := range standings[:limit] {
			if seen[s.DeckID] {
				continue
			}
			seen[s.DeckID] = true
			plan = append(plan, planEntry{event: ev, standing: s})
		}
	}

	logger.Info("fetching decks", "count", len(plan))

	var records []*score.DeckRecord
	miss := 0
	for i, p := range plan {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		deck, err := cfg.MTGTop8.FetchDeck(ctx, p.standing.URL)
		if err != nil {
			logger.Warn("deck fetch failed",
				"deck_id", p.standing.DeckID, "event", p.event.Title, "err", err)
			miss++
			continue
		}
		records = append(records, &score.DeckRecord{
			Event:     p.event,
			Archetype: p.standing.Archetype,
			Cards:     deck.Cards,
		})
		if (i+1)%10 == 0 {
			logger.Debug("deck progress", "fetched", i+1, "total", len(plan))
		}
	}

	cardRecs := score.Compute(records, index, f.Slug, time.Now().UTC())

	tourRefs := make([]*model.TournamentRef, 0, len(filtered))
	for i := range filtered {
		ev := &filtered[i]
		used := 0
		for _, p := range plan {
			if p.event == ev {
				used++
			}
		}
		tourRefs = append(tourRefs, &model.TournamentRef{
			Name:      ev.Title,
			URL:       ev.URL,
			Date:      ev.Date,
			StarTier:  int(ev.TierWeight + 0.5), // approximate display tier
			DeckCount: used,
		})
	}

	if miss > 0 && len(records) == 0 {
		return nil, fmt.Errorf("all %d deck fetches failed", miss)
	}

	logger.Info("format ranked",
		"events", len(filtered),
		"decks_fetched", len(records),
		"deck_misses", miss,
		"cards", len(cardRecs))

	return &model.FormatRanking{
		Slug:        f.Slug,
		DisplayName: f.DisplayName,
		GeneratedAt: time.Now().UTC(),
		DeckCount:   len(records),
		Cards:       cardRecs,
		Tournaments: tourRefs,
	}, nil
}

func sanityCheck(ds *model.Dataset) error {
	if len(ds.Formats) < minFormatsToSucceed {
		return fmt.Errorf("only %d formats produced rankings", len(ds.Formats))
	}
	hasContent := false
	for _, r := range ds.Formats {
		if len(r.Cards) >= 10 {
			hasContent = true
			break
		}
	}
	if !hasContent {
		return fmt.Errorf("no format reached >= 10 cards")
	}
	return nil
}

func (c Config) withDefaults() Config {
	if c.MaxEventsPerFormat == 0 {
		c.MaxEventsPerFormat = defaultMaxEvents
	}
	if c.MaxDecksPerEvent == 0 {
		c.MaxDecksPerEvent = defaultMaxDecks
	}
	if c.MaxEventAgeDays == 0 {
		c.MaxEventAgeDays = defaultMaxAgeDays
	}
	return c
}

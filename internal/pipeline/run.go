package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/francescolofranco-dev/mtga-metacrafter/internal/model"
	"github.com/francescolofranco-dev/mtga-metacrafter/internal/mtggoldfish"
	"github.com/francescolofranco-dev/mtga-metacrafter/internal/score"
	"github.com/francescolofranco-dev/mtga-metacrafter/internal/scryfall"
)

// ErrScrapeDegraded is returned when the scraped data fails sanity checks.
// Callers should keep the previous dataset and try again later.
var ErrScrapeDegraded = errors.New("pipeline: scrape result failed sanity checks")

// Config controls one pipeline run.
type Config struct {
	Scryfall    *scryfall.Client
	MTGGoldfish *mtggoldfish.Client
	Logger      *slog.Logger

	// Formats to scrape. Each must match an MTGGoldfish /tournaments/<slug> URL.
	Formats []mtggoldfish.FormatSpec

	// MaxEventsPerFormat caps the most-recent non-League events we'll consider.
	// Default 5.
	MaxEventsPerFormat int

	// MaxDecksPerEvent caps the deck-fetches per event (taken from the top of
	// the standings table). Default 16.
	MaxDecksPerEvent int

	// MaxEventAgeDays drops events older than this. Default 30.
	MaxEventAgeDays int
}

const (
	defaultMaxEvents    = 10
	defaultMaxDecks     = 32
	defaultMaxAgeDays   = 45
	maxTournamentPages  = 2 // how many MTGGoldfish pages of tournaments to walk
	minFormatsToSucceed = 1
)

// Run fetches every configured format, ranks each one, and joins them into a
// single Dataset. If sanity checks fail, returns ErrScrapeDegraded — callers
// should keep the previous dataset.
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
		SourceLabel: "MTGGoldfish tournaments + Scryfall",
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
	f mtggoldfish.FormatSpec,
	cutoff time.Time,
	logger *slog.Logger,
) (*model.FormatRanking, error) {
	logger.Info("fetching tournaments", "pages", maxTournamentPages)
	var events []mtggoldfish.TournamentEvent
	for page := 1; page <= maxTournamentPages; page++ {
		batch, err := cfg.MTGGoldfish.FetchTournamentsPage(ctx, f.Slug, page)
		if err != nil {
			if page == 1 {
				return nil, fmt.Errorf("tournaments: %w", err)
			}
			logger.Warn("tournaments page fetch failed — continuing with what we have",
				"page", page, "err", err)
			break
		}
		events = append(events, batch...)
		if len(batch) == 0 {
			break
		}
	}

	// Filter by age, sort most-recent-first, cap.
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
	logger.Info("tournaments filtered", "kept", len(filtered), "raw", len(events))

	// For each event, fetch its detail page to get the full standings list
	// (the tournaments index only inlines ~4–8 rows per event). Fall back to
	// the inline standings if the detail fetch fails.
	type planEntry struct {
		event    *mtggoldfish.TournamentEvent
		standing mtggoldfish.DeckStanding
	}
	seen := map[string]bool{}
	var plan []planEntry
	for i := range filtered {
		ev := &filtered[i]
		standings, err := cfg.MTGGoldfish.FetchTournamentStandings(ctx, ev.URL)
		if err != nil || len(standings) == 0 {
			if err != nil {
				logger.Warn("event detail fetch failed; using inline standings",
					"event", ev.Title, "err", err)
			}
			standings = ev.Standings
		}
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
		deck, err := cfg.MTGGoldfish.FetchDeck(ctx, p.standing.DeckURL)
		if err != nil {
			logger.Warn("deck fetch failed",
				"deck_id", p.standing.DeckID,
				"event", p.event.Title,
				"err", err)
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
			StarTier:  ev.StarTier,
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
	// At least one format must yield a non-trivial card list.
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

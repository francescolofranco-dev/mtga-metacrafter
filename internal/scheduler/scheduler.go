package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/francescolofranco-dev/mtga-metacrafter/internal/pipeline"
	"github.com/francescolofranco-dev/mtga-metacrafter/internal/store"
)

// Scheduler runs the pipeline on a periodic ticker and on demand.
type Scheduler struct {
	Period time.Duration
	Cfg    pipeline.Config
	Store  *store.Store
	Logger *slog.Logger

	mu      sync.Mutex
	running bool
	trigger chan struct{}
}

// New returns a Scheduler. Period must be > 0.
func New(period time.Duration, cfg pipeline.Config, st *store.Store, logger *slog.Logger) *Scheduler {
	return &Scheduler{
		Period:  period,
		Cfg:     cfg,
		Store:   st,
		Logger:  logger,
		trigger: make(chan struct{}, 1),
	}
}

// Run drives the loop until ctx is cancelled. Blocks the caller.
func (s *Scheduler) Run(ctx context.Context) {
	// Initial scrape on boot (non-blocking error — log and continue).
	s.runOnce(ctx)

	ticker := time.NewTicker(s.Period)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runOnce(ctx)
		case <-s.trigger:
			s.runOnce(ctx)
		}
	}
}

// Trigger requests an immediate run. Coalesced with any pending trigger.
// Returns true if a run was scheduled; false if one is already pending.
func (s *Scheduler) Trigger() bool {
	select {
	case s.trigger <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *Scheduler) runOnce(ctx context.Context) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		s.Logger.Info("scheduler: skip — previous run still in progress")
		return
	}
	s.running = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	ds, err := pipeline.Run(ctx, s.Cfg)
	if errors.Is(err, pipeline.ErrScrapeDegraded) {
		// Keep the previous dataset — already logged inside pipeline.
		return
	}
	if err != nil {
		s.Logger.Error("scheduler: pipeline error", "err", err)
		return
	}
	if err := s.Store.Swap(ds); err != nil {
		s.Logger.Error("scheduler: store swap failed", "err", err)
		return
	}
	totalCards := 0
	formatSlugs := make([]string, 0, len(ds.Formats))
	for slug, fr := range ds.Formats {
		totalCards += len(fr.Cards)
		formatSlugs = append(formatSlugs, slug)
	}
	s.Logger.Info("scheduler: dataset updated",
		"formats", formatSlugs,
		"total_cards", totalCards,
		"generated_at", ds.GeneratedAt.Format(time.RFC3339))
}

package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/francescolofranco-dev/mtga-metacrafter/internal/mtggoldfish"
	"github.com/francescolofranco-dev/mtga-metacrafter/internal/pipeline"
	"github.com/francescolofranco-dev/mtga-metacrafter/internal/scheduler"
	"github.com/francescolofranco-dev/mtga-metacrafter/internal/scryfall"
	"github.com/francescolofranco-dev/mtga-metacrafter/internal/server"
	"github.com/francescolofranco-dev/mtga-metacrafter/internal/store"
)

func main() {
	cfg := loadConfig()
	logger := newLogger(cfg.LogLevel)

	slugs := make([]string, 0, len(cfg.Formats))
	for _, f := range cfg.Formats {
		slugs = append(slugs, f.Slug)
	}
	logger.Info("metacrafter starting",
		"listen_addr", cfg.ListenAddr,
		"data_dir", cfg.DataDir,
		"refresh_period", cfg.RefreshPeriod.String(),
		"formats", slugs,
		"admin_enabled", cfg.AdminToken != "")

	if err := run(cfg, logger); err != nil {
		logger.Error("fatal", "err", err)
		os.Exit(1)
	}
}

type config struct {
	ListenAddr    string
	DataDir       string
	AdminToken    string
	SeedPath      string
	RefreshPeriod time.Duration
	LogLevel      slog.Level
	Formats       []mtggoldfish.FormatSpec // resolved from FORMATS env
}

func loadConfig() config {
	return config{
		ListenAddr:    envOr("LISTEN_ADDR", ":8080"),
		DataDir:       envOr("DATA_DIR", "./data"),
		AdminToken:    os.Getenv("ADMIN_TOKEN"),
		SeedPath:      envOr("SEED_PATH", "./seed.json"),
		RefreshPeriod: envDuration("REFRESH_PERIOD", 24*time.Hour),
		LogLevel:      parseLevel(envOr("LOG_LEVEL", "info")),
		Formats:       parseFormats(envOr("FORMATS", "standard,pioneer")),
	}
}

// parseFormats turns a comma-separated slug list into FormatSpecs. Unknown
// slugs are skipped (with a stderr line — main logs them via the slog after
// loadConfig).
func parseFormats(s string) []mtggoldfish.FormatSpec {
	var out []mtggoldfish.FormatSpec
	for _, raw := range strings.Split(s, ",") {
		slug := strings.ToLower(strings.TrimSpace(raw))
		if slug == "" {
			continue
		}
		if f, ok := mtggoldfish.FormatBySlug(slug); ok {
			out = append(out, f)
		}
	}
	if len(out) == 0 {
		// Always have at least one — fall back to Standard.
		out = []mtggoldfish.FormatSpec{{Slug: "standard", DisplayName: "Standard"}}
	}
	return out
}

func run(cfg config, logger *slog.Logger) error {
	snapshotPath := filepath.Join(cfg.DataDir, "snapshot.json")
	st, err := store.Open(snapshotPath)
	if err != nil {
		return err
	}
	if err := st.SeedFromFile(cfg.SeedPath); err != nil {
		logger.Warn("seed load failed", "err", err)
	}
	if ds := st.Get(); ds != nil {
		totalCards := 0
		for _, f := range ds.Formats {
			totalCards += len(f.Cards)
		}
		logger.Info("snapshot loaded",
			"formats", len(ds.Formats),
			"total_cards", totalCards,
			"generated_at", ds.GeneratedAt.Format(time.RFC3339))
	} else {
		logger.Info("no snapshot — first scrape will populate the store")
	}

	pipeCfg := pipeline.Config{
		Scryfall:    scryfall.NewClient(logger.With("comp", "scryfall")),
		MTGGoldfish: mtggoldfish.NewClient(logger.With("comp", "mtggoldfish")),
		Logger:      logger,
		Formats:     cfg.Formats,
	}

	sched := scheduler.New(cfg.RefreshPeriod, pipeCfg, st, logger.With("comp", "scheduler"))

	srv, err := server.New(st, sched, logger.With("comp", "server"), cfg.AdminToken, cfg.Formats)
	if err != nil {
		return err
	}

	httpSrv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go sched.Run(ctx)

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.ListenAddr)
		errCh <- httpSrv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}

	shutdownCtx, sc := context.WithTimeout(context.Background(), 10*time.Second)
	defer sc()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Warn("http shutdown", "err", err)
	}
	logger.Info("bye")
	return nil
}

func newLogger(level slog.Level) *slog.Logger {
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	return slog.New(h)
}

func envOr(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	}
	return slog.LevelInfo
}

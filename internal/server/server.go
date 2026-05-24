package server

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/francescolofranco-dev/mtga-metacrafter/internal/model"
	"github.com/francescolofranco-dev/mtga-metacrafter/internal/mtggoldfish"
	"github.com/francescolofranco-dev/mtga-metacrafter/internal/scheduler"
	"github.com/francescolofranco-dev/mtga-metacrafter/internal/store"
)

//go:embed templates/*.html.tmpl
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// Server wires the HTTP surface to the store and scheduler.
type Server struct {
	Store      *store.Store
	Scheduler  *scheduler.Scheduler
	Logger     *slog.Logger
	AdminToken string
	// EnabledFormats is the list shown in the format selector, in display order.
	EnabledFormats []mtggoldfish.FormatSpec

	tmpl *template.Template
}

func New(
	st *store.Store,
	sched *scheduler.Scheduler,
	logger *slog.Logger,
	adminToken string,
	enabledFormats []mtggoldfish.FormatSpec,
) (*Server, error) {
	tmpl, err := template.New("").Funcs(funcMap()).ParseFS(templateFS, "templates/*.html.tmpl")
	if err != nil {
		return nil, fmt.Errorf("server: parse templates: %w", err)
	}
	return &Server{
		Store:          st,
		Scheduler:      sched,
		Logger:         logger,
		AdminToken:     adminToken,
		EnabledFormats: enabledFormats,
		tmpl:           tmpl,
	}, nil
}

// Handler returns the configured http.Handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /rows", s.handleRows)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /admin/refresh", s.handleRefresh)

	staticSub, _ := fs.Sub(staticFS, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	return logRequests(s.Logger, mux)
}

type viewData struct {
	Dataset        *model.Dataset
	Format         *model.FormatRanking // resolved active format (may be nil)
	FormatSlug     string               // active slug
	EnabledFormats []mtggoldfish.FormatSpec
	Rows           []*model.CardRecommendation
	Query          string
	Sort           string
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	slug := s.resolveFormat(r.URL.Query().Get("format"))
	ds := s.Store.Get()
	format := s.formatRanking(ds, slug)
	rows := filterAndSort(format, "", "score")
	s.render(w, "page", viewData{
		Dataset:        ds,
		Format:         format,
		FormatSlug:     slug,
		EnabledFormats: s.EnabledFormats,
		Rows:           rows,
		Sort:           "score",
	})
}

func (s *Server) handleRows(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	sortKey := strings.ToLower(r.URL.Query().Get("sort"))
	if sortKey == "" {
		sortKey = "score"
	}
	slug := s.resolveFormat(r.URL.Query().Get("format"))
	ds := s.Store.Get()
	format := s.formatRanking(ds, slug)
	rows := filterAndSort(format, q, sortKey)
	s.render(w, "rows", viewData{
		Dataset:        ds,
		Format:         format,
		FormatSlug:     slug,
		EnabledFormats: s.EnabledFormats,
		Rows:           rows,
		Query:          q,
		Sort:           sortKey,
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if s.AdminToken == "" {
		http.Error(w, "admin disabled (no ADMIN_TOKEN configured)", http.StatusForbidden)
		return
	}
	if r.Header.Get("X-Admin-Token") != s.AdminToken {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if s.Scheduler.Trigger() {
		_, _ = w.Write([]byte("refresh triggered\n"))
	} else {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("refresh already pending\n"))
	}
}

// resolveFormat picks an enabled format slug. Defaults to the first enabled
// format if the query value is empty or unknown.
func (s *Server) resolveFormat(req string) string {
	req = strings.ToLower(strings.TrimSpace(req))
	for _, f := range s.EnabledFormats {
		if f.Slug == req {
			return req
		}
	}
	if len(s.EnabledFormats) > 0 {
		return s.EnabledFormats[0].Slug
	}
	return "standard"
}

func (s *Server) formatRanking(ds *model.Dataset, slug string) *model.FormatRanking {
	if ds == nil || ds.Formats == nil {
		return nil
	}
	return ds.Formats[slug]
}

func (s *Server) render(w http.ResponseWriter, name string, data viewData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, name, data); err != nil {
		s.Logger.Error("template render failed", "name", name, "err", err)
	}
}

func funcMap() template.FuncMap {
	return template.FuncMap{
		"add":   func(a, b int) int { return a + b },
		"lower": strings.ToLower,
		"trim":  strings.TrimSpace,
		"displayFormat": func(slug string) string {
			if f, ok := mtggoldfish.FormatBySlug(slug); ok {
				return f.DisplayName
			}
			if slug == "" {
				return ""
			}
			return strings.ToUpper(slug[:1]) + slug[1:]
		},
	}
}

func filterAndSort(format *model.FormatRanking, q, sortKey string) []*model.CardRecommendation {
	if format == nil {
		return nil
	}
	q = strings.ToLower(strings.TrimSpace(q))
	out := make([]*model.CardRecommendation, 0, len(format.Cards))
	for _, c := range format.Cards {
		if q != "" && !strings.Contains(strings.ToLower(c.Name), q) {
			continue
		}
		out = append(out, c)
	}
	sortCards(out, sortKey)
	return out
}

func sortCards(cs []*model.CardRecommendation, key string) {
	rank := func(rarity string) int {
		switch rarity {
		case "mythic":
			return 4
		case "rare":
			return 3
		case "uncommon":
			return 2
		case "common":
			return 1
		}
		return 0
	}
	switch key {
	case "name":
		sort.SliceStable(cs, func(i, j int) bool { return cs[i].Name < cs[j].Name })
	case "rarity":
		sort.SliceStable(cs, func(i, j int) bool {
			if rank(cs[i].Rarity) != rank(cs[j].Rarity) {
				return rank(cs[i].Rarity) > rank(cs[j].Rarity)
			}
			return cs[i].Score > cs[j].Score
		})
	default: // score
		sort.SliceStable(cs, func(i, j int) bool {
			if cs[i].Score != cs[j].Score {
				return cs[i].Score > cs[j].Score
			}
			return cs[i].Name < cs[j].Name
		})
	}
}

func logRequests(logger *slog.Logger, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger.Debug("http", "method", r.Method, "path", r.URL.Path, "query", r.URL.RawQuery)
		h.ServeHTTP(w, r)
	})
}

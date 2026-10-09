package web

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/USA-RedDragon/wx/internal/config"
	"github.com/USA-RedDragon/wx/internal/noaa"
	"github.com/USA-RedDragon/wx/internal/plot"
	"github.com/USA-RedDragon/wx/internal/store"
)

//go:embed static templates
var assets embed.FS

const (
	defTimeout   = 10 * time.Second
	writeTimeout = 30 * time.Second
	stopTimeout  = 5 * time.Second
	pageMaxAge   = 30 * time.Second
	noaaMaxAge   = 10 * time.Minute
	roseMaxAge   = time.Minute
	contentSVG   = "image/svg+xml"
	contentHTML  = "text/html; charset=utf-8"
	contentText  = "text/plain; charset=utf-8"
	headerCType  = "Content-Type"
	headerCache  = "Cache-Control"
	windRoseName = "daywindrose"
)

var noaaFile = regexp.MustCompile(`^NOAA-(\d{4})(?:-(\d{2}))?\.txt$`)

type cached struct {
	gen  uint64
	at   time.Time
	body []byte
}

type Server struct {
	cfg     *config.Config
	store   *store.Store
	nws     *NWS
	tmpl    *template.Template
	server  *http.Server
	version string
	started time.Time
	mu      sync.Mutex
	cache   map[string]cached
	stopped bool
}

func plotMaxAge(period string) time.Duration {
	switch period {
	case plot.PeriodDay:
		return time.Minute
	case plot.PeriodWeek:
		return 5 * time.Minute
	case plot.PeriodMonth:
		return 30 * time.Minute
	}
	return 6 * time.Hour
}

func New(cfg *config.Config, st *store.Store, nws *NWS, version string) (*Server, error) {
	s := &Server{cfg: cfg, store: st, nws: nws, version: version, started: time.Now(), cache: map[string]cached{}}
	funcs := template.FuncMap{
		"safe":      func(v string) template.HTML { return template.HTML(v) }, //nolint:gosec
		"hourLabel": func(t time.Time) string { return t.In(cfg.Station.Timezone).Format("_3 PM") },
		"longTime":  func(t time.Time) string { return t.In(cfg.Station.Timezone).Format(fmtLong) },
		"tempClass": func(f float64) string {
			switch {
			case f < 50:
				return "temp-cold"
			case f > 78:
				return "temp-hot"
			}
			return "temp-norm"
		},
		"firstN": func(n int, p []ForecastPeriod) []ForecastPeriod {
			if len(p) > n {
				return p[:n]
			}
			return p
		},
	}
	t, err := template.New("").Funcs(funcs).ParseFS(assets, "templates/*.html")
	if err != nil {
		return nil, err
	}
	s.tmpl = t
	static, err := fs.Sub(assets, "static")
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	fileServer := http.FileServerFS(static)
	mux.HandleFunc("GET /{$}", s.page("index", func(ctx context.Context) (any, error) { return s.buildIndex(ctx) }))
	mux.HandleFunc("GET /index.html", s.page("index", func(ctx context.Context) (any, error) { return s.buildIndex(ctx) }))
	mux.HandleFunc("GET /statistics.html", s.page("statistics", func(ctx context.Context) (any, error) { return s.buildStatistics(ctx) }))
	mux.HandleFunc("GET /telemetry.html", s.page("telemetry", func(ctx context.Context) (any, error) { return s.buildTelemetry(ctx) }))
	mux.HandleFunc("GET /celestial.html", s.page("celestial", func(ctx context.Context) (any, error) { return s.buildCelestial(ctx) }))
	mux.HandleFunc("GET /tabular.html", s.page("tabular", func(ctx context.Context) (any, error) {
		pc, err := s.context(ctx)
		if err != nil {
			return nil, err
		}
		return s.header(ctx, pc)
	}))
	mux.HandleFunc("GET /NOAA/{file}", s.handleNOAA)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /"+windRoseName+".svg", s.handleWindRose)
	mux.HandleFunc("GET /{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if strings.HasSuffix(name, ".svg") {
			s.handlePlot(w, r, strings.TrimSuffix(name, ".svg"))
			return
		}
		w.Header().Set(headerCache, "public, max-age=3600")
		fileServer.ServeHTTP(w, r)
	})
	mux.Handle("GET /font/", fileServer)
	s.server = &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.HTTP.Bind, cfg.HTTP.Port),
		ReadHeaderTimeout: defTimeout,
		WriteTimeout:      writeTimeout,
		Handler:           mux,
	}
	return s, nil
}

func (s *Server) Handler() http.Handler {
	return s.server.Handler
}

func (s *Server) cachedRender(ctx context.Context, key string, maxAge time.Duration, render func(context.Context) ([]byte, error)) ([]byte, error) {
	gen := s.store.Generation()
	s.mu.Lock()
	c, ok := s.cache[key]
	s.mu.Unlock()
	if ok && (c.gen == gen || time.Since(c.at) < maxAge) {
		return c.body, nil
	}
	body, err := render(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.cache[key] = cached{gen: gen, at: time.Now(), body: body}
	s.mu.Unlock()
	return body, nil
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, what string, err error) {
	if errors.Is(err, store.ErrNoData) {
		http.Error(w, "no data yet", http.StatusServiceUnavailable)
		return
	}
	slog.Error("render failed", "what", what, "path", r.URL.Path, "error", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

func (s *Server) page(name string, build func(context.Context) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := s.cachedRender(r.Context(), "page:"+name, pageMaxAge, func(ctx context.Context) ([]byte, error) {
			data, err := build(ctx)
			if err != nil {
				return nil, err
			}
			var b bytes.Buffer
			if err := s.tmpl.ExecuteTemplate(&b, name, data); err != nil {
				return nil, err
			}
			return b.Bytes(), nil
		})
		if err != nil {
			s.fail(w, r, name, err)
			return
		}
		w.Header().Set(headerCType, contentHTML)
		_, _ = w.Write(body)
	}
}

func (s *Server) station() noaa.Station {
	return noaa.Station{
		Location:     s.cfg.Station.Location,
		Latitude:     s.cfg.Station.Latitude,
		Longitude:    s.cfg.Station.Longitude,
		AltitudeFeet: s.cfg.Station.AltitudeFeet,
		Loc:          s.cfg.Station.Timezone,
	}
}

func (s *Server) handleNOAA(w http.ResponseWriter, r *http.Request) {
	m := noaaFile.FindStringSubmatch(r.PathValue("file"))
	if m == nil {
		http.NotFound(w, r)
		return
	}
	year, _ := strconv.Atoi(m[1])
	body, err := s.cachedRender(r.Context(), "noaa:"+m[0], noaaMaxAge, func(ctx context.Context) ([]byte, error) {
		if m[2] == "" {
			txt, err := noaa.Year(ctx, s.store, s.station(), year)
			return []byte(txt), err
		}
		month, _ := strconv.Atoi(m[2])
		if month < 1 || month > 12 {
			return nil, errNotFound
		}
		txt, err := noaa.Month(ctx, s.store, s.station(), year, time.Month(month))
		return []byte(txt), err
	})
	if errors.Is(err, errNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.fail(w, r, "noaa", err)
		return
	}
	w.Header().Set(headerCType, contentText)
	_, _ = w.Write(body)
}

var errNotFound = errors.New("not found")

func (s *Server) handlePlot(w http.ResponseWriter, r *http.Request, name string) {
	var period plot.Period
	var group string
	for _, p := range plot.Periods() {
		if strings.HasPrefix(name, p.Name) {
			period, group = p, strings.TrimPrefix(name, p.Name)
			break
		}
	}
	spec, ok := plot.Specs(period.Name)[group]
	if period.Name == "" || !ok {
		http.NotFound(w, r)
		return
	}
	body, err := s.cachedRender(r.Context(), "plot:"+name, plotMaxAge(period.Name), func(ctx context.Context) ([]byte, error) {
		return s.renderPlot(ctx, period, spec)
	})
	if err != nil {
		if errors.Is(err, store.ErrNoData) {
			http.NotFound(w, r)
			return
		}
		s.fail(w, r, name, err)
		return
	}
	w.Header().Set(headerCType, contentSVG)
	w.Header().Set(headerCache, "public, max-age=60")
	_, _ = w.Write(body)
}

func (s *Server) renderPlot(ctx context.Context, period plot.Period, spec plot.Spec) ([]byte, error) {
	latest, err := s.store.Latest(ctx)
	if err != nil {
		return nil, err
	}
	end := latest.DateTime
	start := end - int64(period.Length/time.Second)
	fr := plot.Frame{
		Start:       start,
		End:         end,
		Loc:         s.cfg.Station.Timezone,
		Lat:         s.cfg.Station.Latitude,
		Lng:         s.cfg.Station.Longitude,
		BottomLabel: time.Unix(end, 0).In(s.cfg.Station.Timezone).Format(fmtLong),
		Period:      period,
		Spec:        spec,
	}
	b := plot.NewBuilder(fr)
	if err := s.store.Each(ctx, plot.Columns(spec), start, end, b.Add); err != nil {
		return nil, err
	}
	return plot.Render(fr, b.Data()), nil
}

func (s *Server) handleWindRose(w http.ResponseWriter, r *http.Request) {
	body, err := s.cachedRender(r.Context(), "plot:"+windRoseName, roseMaxAge, func(ctx context.Context) ([]byte, error) {
		latest, err := s.store.Latest(ctx)
		if err != nil {
			return nil, err
		}
		var recs []store.Record
		err = s.store.Each(ctx, []string{obsWindSpeed, obsWindDir}, latest.DateTime-86400, latest.DateTime, func(rec store.Record) {
			v := make(map[string]float64, len(rec.Values))
			for k, x := range rec.Values {
				v[k] = x
			}
			recs = append(recs, store.Record{DateTime: rec.DateTime, Values: v})
		})
		if err != nil {
			return nil, err
		}
		stamp := time.Unix(latest.DateTime, 0).In(s.cfg.Station.Timezone).Format("15:04 2 Jan 06")
		return plot.WindRose(recs, "24 Hour Wind Rose", stamp), nil
	})
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set(headerCType, contentSVG)
	_, _ = w.Write(body)
}

func (s *Server) Start() error {
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", s.server.Addr)
	if err != nil {
		return err
	}
	go func() {
		if err := s.server.Serve(ln); err != nil && !s.stopped {
			slog.Error("HTTP server error", "error", err.Error())
		}
	}()
	slog.Info("HTTP server started", "address", s.cfg.HTTP.Bind, "port", s.cfg.HTTP.Port)
	return nil
}

func (s *Server) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
	defer cancel()
	s.stopped = true
	return s.server.Shutdown(ctx)
}

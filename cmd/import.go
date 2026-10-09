package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/USA-RedDragon/wx/internal/config"
	"github.com/USA-RedDragon/wx/internal/importer"
	"github.com/USA-RedDragon/wx/internal/store"
	"github.com/spf13/cobra"
)

const (
	flagWeewx     = "weewx"
	flagWx        = "wx"
	flagPromDir   = "prometheus-dir"
	flagPromURL   = "prometheus-url"
	flagHAFiles   = "homeassistant"
	flagHAURL     = "homeassistant-url"
	flagSince     = "since"
	flagUntil     = "until"
	envHAToken    = "HA_TOKEN"
	sinceExample  = "2026-09-17T00:00:00Z"
	timeLayoutDay = "2006-01-02"
	fetchWindow   = 24 * time.Hour
	fetchOverlap  = time.Hour
)

var errSinceRequired = errors.New("--since is required when fetching from Prometheus or Home Assistant")

func newImportCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import history from weewx archives, Prometheus and Home Assistant statistics",
		Long:  "Import history from weewx archives, Prometheus and Home Assistant statistics. Sources are applied in priority order (weewx, Prometheus, Home Assistant) and lower priority sources only fill archive intervals that are still empty. The Home Assistant token is read from the " + envHAToken + " environment variable.",
		RunE:  runImport,
	}
	cmd.Flags().StringSlice(flagWx, nil, "Another wx database to merge, keeping each record's source")
	cmd.Flags().StringSlice(flagWeewx, nil, "weewx SQLite archive to import, highest priority first")
	cmd.Flags().String(flagPromDir, "", "Directory of per-entity Prometheus TSV exports")
	cmd.Flags().String(flagPromURL, "", "Prometheus or Thanos query URL to fetch Home Assistant sensor series from")
	cmd.Flags().StringSlice(flagHAFiles, nil, "Home Assistant hourly statistics JSON file")
	cmd.Flags().String(flagHAURL, "", "Home Assistant URL to fetch hourly long-term statistics from")
	cmd.Flags().String(flagSince, "", "Start of the window fetched from Prometheus or Home Assistant, RFC 3339 or YYYY-MM-DD, for example "+sinceExample)
	cmd.Flags().String(flagUntil, "", "End of the window fetched from Prometheus or Home Assistant, defaults to now")
	return cmd
}

func parseTime(s string, def time.Time) (time.Time, error) {
	if s == "" {
		return def, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Parse(timeLayoutDay, s)
}

type importRun struct {
	cfg      *config.Config
	st       *store.Store
	interval int64
	first    int64
	last     int64
}

func (r *importRun) track(res importer.Result) {
	if res.Inserted == 0 {
		return
	}
	if r.first == 0 || res.First < r.first {
		r.first = res.First
	}
	if res.Last > r.last {
		r.last = res.Last
	}
	fmt.Println(res.String())
}

func runImport(cmd *cobra.Command, _ []string) error {
	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}
	st, err := store.Open(cmd.Context(), cfg.Database.Path)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	ctx := cmd.Context()
	run := &importRun{cfg: cfg, st: st, interval: int64(cfg.Station.ArchiveSecs)}

	wxFiles, _ := cmd.Flags().GetStringSlice(flagWx)
	for _, f := range wxFiles {
		res, err := importer.ImportWxDatabase(ctx, st, f)
		if err != nil {
			return fmt.Errorf("merge %s: %w", f, err)
		}
		run.track(res)
	}
	weewxFiles, _ := cmd.Flags().GetStringSlice(flagWeewx)
	for _, f := range weewxFiles {
		res, err := importer.ImportWeewxSQLite(ctx, st, f, run.interval, cfg.Station.AltitudeFeet)
		if err != nil {
			return fmt.Errorf("import %s: %w", f, err)
		}
		run.track(res)
	}
	if err := run.prometheus(ctx, cmd); err != nil {
		return err
	}
	if err := run.homeAssistant(ctx, cmd); err != nil {
		return err
	}
	if run.first == 0 {
		slog.Info("nothing new was imported")
		return nil
	}
	slog.Info("rebuilding daily summaries", "from", time.Unix(run.first, 0), "to", time.Unix(run.last, 0))
	return st.RebuildDaysBetween(ctx, cfg.Station.Timezone, run.first, run.last)
}

func (r *importRun) window(cmd *cobra.Command) (time.Time, time.Time, error) {
	sinceStr, _ := cmd.Flags().GetString(flagSince)
	untilStr, _ := cmd.Flags().GetString(flagUntil)
	if sinceStr == "" {
		return time.Time{}, time.Time{}, errSinceRequired
	}
	since, err := parseTime(sinceStr, time.Time{})
	if err != nil {
		return since, since, err
	}
	until, err := parseTime(untilStr, time.Now())
	return since, until, err
}

func (r *importRun) prometheus(ctx context.Context, cmd *cobra.Command) error {
	dir, _ := cmd.Flags().GetString(flagPromDir)
	base, _ := cmd.Flags().GetString(flagPromURL)
	var series importer.Series
	var name string
	switch {
	case dir != "":
		s, err := importer.LoadPrometheusDir(dir)
		if err != nil {
			return err
		}
		series, name = s, "prometheus "+dir
	case base != "":
		return r.prometheusURL(ctx, cmd, base)
	default:
		return nil
	}
	recs := importer.PrometheusRecords(series, r.interval, r.cfg.Station.AltitudeFeet)
	res, err := importer.ImportRecordsGapFill(ctx, r.st, recs, name)
	if err != nil {
		return err
	}
	r.track(res)
	return nil
}

func (r *importRun) homeAssistant(ctx context.Context, cmd *cobra.Command) error {
	files, _ := cmd.Flags().GetStringSlice(flagHAFiles)
	base, _ := cmd.Flags().GetString(flagHAURL)
	var stats map[string][]importer.HAStat
	var name string
	switch {
	case len(files) > 0:
		s, err := importer.LoadHAStats(files)
		if err != nil {
			return err
		}
		stats, name = s, "homeassistant files"
	case base != "":
		return r.homeAssistantURL(ctx, cmd, base)
	default:
		return nil
	}
	res, err := importer.ImportRecordsGapFill(ctx, r.st, importer.HAHourRecords(stats, r.cfg.Station.AltitudeFeet), name)
	if err != nil {
		return err
	}
	r.track(res)
	return nil
}

func (r *importRun) prometheusURL(ctx context.Context, cmd *cobra.Command, base string) error {
	since, until, err := r.window(cmd)
	if err != nil {
		return err
	}
	total := importer.Result{Source: "prometheus " + base}
	for from := since; from.Before(until); from = from.Add(fetchWindow) {
		to := from.Add(fetchWindow)
		if to.After(until) {
			to = until
		}
		series, err := importer.FetchPrometheus(ctx, base, from.Add(-fetchOverlap), to)
		if err != nil {
			return err
		}
		all := importer.PrometheusRecords(series, r.interval, r.cfg.Station.AltitudeFeet)
		recs := all[:0]
		for _, rec := range all {
			if rec.DateTime > from.Unix() {
				recs = append(recs, rec)
			}
		}
		res, err := importer.ImportRecordsGapFill(ctx, r.st, recs, total.Source)
		if err != nil {
			return err
		}
		total.Add(res)
	}
	r.track(total)
	return nil
}

func (r *importRun) homeAssistantURL(ctx context.Context, cmd *cobra.Command, base string) error {
	since, until, err := r.window(cmd)
	if err != nil {
		return err
	}
	total := importer.Result{Source: "homeassistant " + base}
	err = importer.StreamHAStatistics(ctx, base, os.Getenv(envHAToken), since, until, func(part map[string][]importer.HAStat) error {
		res, err := importer.ImportRecordsGapFill(ctx, r.st, importer.HAHourRecords(part, r.cfg.Station.AltitudeFeet), total.Source)
		total.Add(res)
		return err
	})
	if err != nil {
		return err
	}
	r.track(total)
	return nil
}

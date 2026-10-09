package store_test

import (
	"context"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/USA-RedDragon/wx/internal/store"
	"github.com/USA-RedDragon/wx/internal/wx"
	_ "modernc.org/sqlite"
)

func open(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "wx.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestLiveSummariesMatchRebuild(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	loc, _ := time.LoadLocation("America/Chicago")
	s := open(t)
	base := time.Date(2026, 3, 7, 23, 0, 0, 0, loc).Unix()
	for i := range 240 {
		ts := base + int64(i+1)*60
		r := store.Record{DateTime: ts, Interval: 1, Values: map[string]float64{
			wx.OutTemp:   50 + 10*math.Sin(float64(i)/20),
			wx.WindSpeed: float64(i % 7),
			wx.WindDir:   float64((i * 15) % 360),
			wx.WindGust:  float64(i%7) + 2,
			wx.Rain:      0.01 * float64(i%3),
		}}
		if err := s.AddLive(ctx, r, loc); err != nil {
			t.Fatal(err)
		}
	}
	day := time.Date(2026, 3, 8, 0, 0, 0, 0, loc).Unix()
	live, err := s.Stats(ctx, wx.OutTemp, day, day+86400)
	if err != nil {
		t.Fatal(err)
	}
	liveWind, _ := s.Stats(ctx, "wind", day, day+86400)
	liveRain, _ := s.Stats(ctx, wx.Rain, day, day+86400)
	if err := s.RebuildDays(ctx, loc); err != nil {
		t.Fatal(err)
	}
	rebuilt, _ := s.Stats(ctx, wx.OutTemp, day, day+86400)
	rebuiltRain, _ := s.Stats(ctx, wx.Rain, day, day+86400)
	if *live.Max != *rebuilt.Max || *live.Min != *rebuilt.Min || math.Abs(*live.Avg-*rebuilt.Avg) > 1e-9 {
		t.Errorf("live %+v rebuilt %+v", live, rebuilt)
	}
	if math.Abs(*liveRain.Sum-*rebuiltRain.Sum) > 1e-9 || *liveRain.Sum <= 0 {
		t.Errorf("rain live %v rebuilt %v", *liveRain.Sum, *rebuiltRain.Sum)
	}
	if liveWind.Max == nil || *liveWind.Max != 8 || liveWind.MaxDir == nil || liveWind.VecDir == nil || liveWind.RMS == nil {
		t.Errorf("wind %+v", liveWind)
	}
	prev, _ := s.Stats(ctx, wx.OutTemp, day-86400, day)
	if prev.Count != 60 || live.Count != 180 {
		t.Errorf("day split prev=%d today=%d", prev.Count, live.Count)
	}
}

func TestInsertIgnoresDuplicatesAndCoverage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := open(t)
	r := store.Record{DateTime: 600, Interval: 1, Source: store.SourceWeewx, Values: map[string]float64{wx.InTemp: 70}}
	n, err := s.InsertRecords(ctx, []store.Record{r, r}, false)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if c, _ := s.Covered(ctx, 540, 600); !c {
		t.Error("expected covered")
	}
	if c, _ := s.Covered(ctx, 600, 3600); c {
		t.Error("expected not covered")
	}
	latest, err := s.Latest(ctx)
	if err != nil || latest.Source != store.SourceWeewx || latest.Values[wx.InTemp] != 70 {
		t.Errorf("latest %+v %v", latest, err)
	}
	if _, ok, _ := s.LastSeen(ctx, wx.OutTemp, 0); ok {
		t.Error("outTemp never seen")
	}
	if err := s.SetMeta(ctx, "k", "v"); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.Meta(ctx, "k"); v != "v" {
		t.Errorf("meta %q", v)
	}
}

func TestEmptyStore(t *testing.T) {
	t.Parallel()
	s := open(t)
	if _, err := s.Latest(context.Background()); err == nil {
		t.Error("expected ErrNoData")
	}
}

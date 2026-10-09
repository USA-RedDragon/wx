package plot_test

import (
	"strings"
	"testing"
	"time"

	"github.com/USA-RedDragon/wx/internal/plot"
	"github.com/USA-RedDragon/wx/internal/store"
	"github.com/USA-RedDragon/wx/internal/wx"
)

func TestBuildAggregatesHourlyRain(t *testing.T) {
	t.Parallel()
	loc := time.UTC
	per, _ := plot.PeriodByName("week")
	fr := plot.Frame{Start: 0, End: 7 * 86400, Loc: loc, Period: per, Spec: plot.Specs("week")[wx.Rain]}
	recs := make([]store.Record, 0, 180)
	for i := range 180 {
		recs = append(recs, store.Record{DateTime: int64(i+1) * 60, Interval: 1, Values: map[string]float64{wx.Rain: 0.01}})
	}
	d := plot.Build(recs, fr)
	if len(d.Series) != 1 || len(d.Series[0]) != 1 {
		t.Fatalf("series %+v", d.Series)
	}
	if p := d.Series[0][0]; p.T != 86400 || p.V < 1.79 || p.V > 1.81 {
		t.Errorf("point %+v", p)
	}
	svg := string(plot.Render(fr, d))
	if !strings.HasPrefix(svg, "<svg") || !strings.Contains(svg, "Rain (daily total)") {
		t.Error("bad svg")
	}
}

func TestWindRose(t *testing.T) {
	t.Parallel()
	recs := []store.Record{
		{Values: map[string]float64{wx.WindSpeed: 5, wx.WindDir: 180}},
		{Values: map[string]float64{wx.WindSpeed: 0}},
	}
	svg := string(plot.WindRose(recs, "24 Hour Wind Rose", "now"))
	if !strings.Contains(svg, "50%") || !strings.Contains(svg, "Calm") {
		t.Error("windrose missing calm percentage")
	}
}

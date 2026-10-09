package archive_test

import (
	"math"
	"testing"

	"github.com/USA-RedDragon/wx/internal/archive"
	"github.com/USA-RedDragon/wx/internal/store"
	"github.com/USA-RedDragon/wx/internal/wx"
)

func TestRainTracker(t *testing.T) {
	t.Parallel()
	var r archive.RainTracker
	steps := []struct{ total, want float64 }{
		{10, 0}, {10, 0}, {12.5, 2.5}, {13, 0.5}, {4, 0}, {4.2, 0.2}, {0, 0},
	}
	for i, s := range steps {
		if got := r.Update(s.total); math.Abs(got-s.want) > 1e-9 {
			t.Errorf("step %d: Update(%v) = %v, want %v", i, s.total, got, s.want)
		}
	}
}

func TestAccumulatorAggregates(t *testing.T) {
	t.Parallel()
	a := archive.NewAccumulator(60)
	recs := make([]store.Record, 0, 2)
	recs = append(recs, a.AddPacket(1000, map[string]float64{wx.OutTemp: 50, wx.WindSpeed: 4, wx.WindDir: 90, wx.WindGust: 6})...)
	recs = append(recs, a.AddPacket(1010, map[string]float64{wx.OutTemp: 52, wx.WindSpeed: 4, wx.WindDir: 90, wx.WindGust: 9})...)
	recs = append(recs, a.AddPacket(1015, map[string]float64{wx.OutTemp: 54, wx.WindSpeed: 0, wx.WindGust: 3, wx.LightningStrikeCount: 5})...)
	recs = append(recs, a.AddRain(1016, 0.01)...)
	recs = append(recs, a.AddRain(1017, 0.02)...)
	if len(recs) != 0 {
		t.Fatalf("unexpected early records %v", recs)
	}
	recs = append(recs, a.AddPacket(1030, map[string]float64{wx.LightningStrikeCount: 7})...)
	recs = append(recs, a.AddPacket(1081, map[string]float64{wx.OutTemp: 70})...)
	if len(recs) != 2 {
		t.Fatalf("got %d records", len(recs))
	}
	r := recs[0]
	if r.DateTime != 1020 || r.Interval != 1 {
		t.Errorf("record time %d interval %d", r.DateTime, r.Interval)
	}
	checks := map[string]float64{wx.OutTemp: 52, wx.WindSpeed: 8.0 / 3, wx.WindGust: 9, wx.WindGustDir: 90, wx.WindDir: 90, wx.Rain: 0.03, wx.RainRate: 0.12}
	for k, want := range checks {
		if got, ok := r.Values[k]; !ok || math.Abs(got-want) > 1e-6 {
			t.Errorf("%s = %v (%v), want %v", k, got, ok, want)
		}
	}
	if got := recs[1].Values[wx.LightningStrikeCount]; recs[1].DateTime != 1080 || got != 2 {
		t.Errorf("second record %+v", recs[1])
	}
	cur, ts := a.Current()
	if ts != 1081 || cur[wx.OutTemp] != 70 {
		t.Errorf("current = %v at %d", cur, ts)
	}
}

func TestAccumulatorTickEmitsEmptyIntervalOnce(t *testing.T) {
	t.Parallel()
	a := archive.NewAccumulator(60)
	a.AddPacket(100, map[string]float64{wx.InTemp: 70})
	if recs := a.Tick(119); len(recs) != 0 {
		t.Fatal("record emitted before boundary")
	}
	if recs := a.Tick(121); len(recs) != 1 {
		t.Fatalf("got %d records", len(recs))
	}
	if recs := a.Tick(200); len(recs) != 0 {
		t.Fatal("empty interval should not produce a record")
	}
}

func TestParseStationPacket(t *testing.T) {
	t.Parallel()
	p, err := archive.ParseStationPacket([]byte(`{"outTemp": 20, "windSpeed": 1, "rain": 2.54, "dateTime": 1791577482.0, "barometer": 1000, "bogus": 1, "name": "x"}`))
	if err != nil {
		t.Fatal(err)
	}
	if p.DateTime != 1791577482 || p.RainMM == nil || *p.RainMM != 2.54 {
		t.Errorf("packet %+v", p)
	}
	if math.Abs(p.Values[wx.OutTemp]-68) > 1e-9 || math.Abs(p.Values[wx.Pressure]-29.53) > 0.01 {
		t.Errorf("values %v", p.Values)
	}
	if _, ok := p.Values["bogus"]; ok {
		t.Error("unknown field kept")
	}
	if _, err := archive.ParseStationPacket([]byte(`nope`)); err == nil {
		t.Error("expected error")
	}
	mm, err := archive.ParseRainCounter([]byte(`{"model":"Cotech-367959","rain_mm":123.4}`))
	if err != nil || mm != 123.4 {
		t.Errorf("rain counter %v %v", mm, err)
	}
}

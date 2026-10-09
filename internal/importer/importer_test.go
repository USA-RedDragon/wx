package importer_test

import (
	"math"
	"testing"

	"github.com/USA-RedDragon/wx/internal/importer"
	"github.com/USA-RedDragon/wx/internal/wx"
)

func f(v float64) *float64 { return &v }

func TestBucketerMinute(t *testing.T) {
	t.Parallel()
	b := importer.NewBucketer(60, 1)
	b.Add(1, map[string]float64{wx.OutTemp: 50, wx.Rain: 0.01, wx.WindSpeed: 2, wx.WindDir: 10, wx.WindGust: 3})
	b.Add(30, map[string]float64{wx.OutTemp: 60, wx.Rain: 0.02, wx.WindSpeed: 2, wx.WindDir: 350, wx.WindGust: 7})
	b.Add(60, map[string]float64{wx.OutTemp: 70})
	b.Add(61, map[string]float64{wx.OutTemp: 10})
	recs := b.Drain(true)
	if len(recs) != 2 {
		t.Fatalf("got %d records", len(recs))
	}
	r := recs[0]
	if r.DateTime != 60 || r.Values[wx.OutTemp] != 60 || math.Abs(r.Values[wx.Rain]-0.03) > 1e-9 || r.Values[wx.WindGust] != 7 {
		t.Errorf("record %+v", r)
	}
	if d := r.Values[wx.WindDir]; d > 1 && d < 359 {
		t.Errorf("vector average across north = %v", d)
	}
	if r.Hi[wx.OutTemp].Value != 70 || r.Lo[wx.OutTemp].Value != 50 || r.Hi[wx.OutTemp].Time != 60 {
		t.Errorf("extremes %+v %+v", r.Hi, r.Lo)
	}
}

func TestDropStale(t *testing.T) {
	t.Parallel()
	ts := make([]int64, 0, 100)
	vals := make([]map[string]float64, 0, 100)
	for i := range 100 {
		ts = append(ts, int64(i*60))
		v := 70.0
		if i < 5 || i > 90 {
			v = float64(i)
		}
		vals = append(vals, map[string]float64{wx.OutTemp: v, wx.WindSpeed: 1, wx.InTemp: float64(i)})
	}
	n := importer.DropStale(ts, vals, wx.OutTemp, []string{wx.OutTemp, wx.WindSpeed}, 1800)
	if n == 0 {
		t.Fatal("nothing dropped")
	}
	if _, ok := vals[5][wx.OutTemp]; !ok {
		t.Error("first value of a run should be kept")
	}
	if _, ok := vals[50][wx.OutTemp]; ok {
		t.Error("stale value kept")
	}
	if _, ok := vals[50][wx.InTemp]; !ok {
		t.Error("unrelated field dropped")
	}
	if _, ok := vals[95][wx.OutTemp]; !ok {
		t.Error("fresh value dropped")
	}
}

func TestHAHourRecords(t *testing.T) {
	t.Parallel()
	stats := map[string][]importer.HAStat{
		"sensor.outside_temperature":                    {{End: 3600000, Mean: f(80), Min: f(75), Max: f(85)}},
		"sensor.outside_humidity":                       {{End: 3600000, Mean: f(50), Min: f(40), Max: f(60)}},
		"sensor.wind_direction":                         {{End: 3600000, Mean: f(180), Min: f(10), Max: f(350)}},
		"sensor.living_room_temperature":                {{End: 3600000, Mean: f(70)}},
		"sensor.indoor_sensors_living_room_temperature": {{End: 3600000, Mean: f(71)}},
		"sensor.indoor_sensors_ccs811_tvoc":             {{End: 3600000, Mean: f(100)}},
		"sensor.indoor_sensors_outside_pressure":        {{End: 3600000, Mean: f(28.71)}},
		"sensor.total_precipitation":                    {{End: 3600000, Change: f(0.25)}, {End: 7200000, Change: f(-4)}, {End: 10800000, Change: f(9)}},
	}
	recs := importer.HAHourRecords(stats, 1191)
	if len(recs) != 1 {
		t.Fatalf("got %d records", len(recs))
	}
	v := recs[0].Values
	if v[wx.OutTemp] != 80 || v[wx.InTemp] != 71 || v[wx.Rain] != 0.25 || math.Abs(v[wx.TVOC]-0.1) > 1e-9 {
		t.Errorf("values %v", v)
	}
	if _, ok := v[wx.WindDir]; ok {
		t.Error("wind direction spanning north should be dropped")
	}
	if v[wx.Pressure] != 28.71 || v[wx.Barometer] < 29.9 || v[wx.Barometer] > 30.1 || v[wx.Altimeter] < 29.9 || v[wx.Altimeter] > 30.1 {
		t.Errorf("pressure %v barometer %v altimeter %v", v[wx.Pressure], v[wx.Barometer], v[wx.Altimeter])
	}
	if _, ok := v[wx.Dewpoint]; !ok {
		t.Error("dewpoint not derived")
	}
	if recs[0].Hi[wx.OutTemp].Value != 85 || recs[0].Interval != 60 {
		t.Errorf("hi %+v interval %d", recs[0].Hi, recs[0].Interval)
	}
}

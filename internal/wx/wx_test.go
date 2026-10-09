package wx_test

import (
	"math"
	"testing"

	"github.com/USA-RedDragon/wx/internal/wx"
)

func ptr(v float64) *float64 { return &v }

func TestFromMetricWX(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   float64
		want float64
	}{
		{wx.OutTemp, 0, 32},
		{wx.OutTemp, 100, 212},
		{wx.WindSpeed, 1, 2.2369362920544},
		{wx.Rain, 25.4, 1},
		{wx.Barometer, 1013.25, 29.9213},
		{wx.OutHumidity, 55, 55},
	}
	for _, tt := range tests {
		got := wx.FromMetricWX(tt.name, tt.in)
		if math.Abs(got-tt.want) > 1e-3 {
			t.Errorf("FromMetricWX(%s, %v) = %v, want %v", tt.name, tt.in, got, tt.want)
		}
	}
}

func TestFormatValue(t *testing.T) {
	t.Parallel()
	if got := wx.FormatValue(wx.OutTemp, ptr(28.94), true); got != "28.9°F" {
		t.Errorf("got %q", got)
	}
	if got := wx.FormatValue(wx.Rain, ptr(0), true); got != "0.00 in" {
		t.Errorf("got %q", got)
	}
	if got := wx.FormatValue(wx.WindDir, ptr(26), false); got != "026" {
		t.Errorf("got %q", got)
	}
	if got := wx.FormatValue(wx.OutTemp, nil, true); got != "N/A" {
		t.Errorf("got %q", got)
	}
	if got := wx.FormatValue(wx.OutTemp, ptr(-0.04), false); got != "0.0" {
		t.Errorf("got %q", got)
	}
}

func TestOrdinal(t *testing.T) {
	t.Parallel()
	cases := map[float64]string{0: "N", 11: "N", 12: "NNE", 90: "E", 258: "WSW", 349: "N", 359.9: "N"}
	for d, want := range cases {
		if got := wx.Ordinal(ptr(d)); got != want {
			t.Errorf("Ordinal(%v) = %q, want %q", d, got, want)
		}
	}
	if got := wx.Ordinal(nil); got != "N/A" {
		t.Errorf("got %q", got)
	}
}

func TestDerived(t *testing.T) {
	t.Parallel()
	if d := wx.DewpointF(93.9, 29); math.Abs(d-57.5) > 1.5 {
		t.Errorf("dewpoint = %v", d)
	}
	if hi := wx.HeatIndexF(93.9, 29); math.Abs(hi-92.5) > 1 {
		t.Errorf("heat index = %v", hi)
	}
	if _, ok := wx.WindChillF(60, 10); ok {
		t.Error("wind chill should not apply above 50F")
	}
	if wc, ok := wx.WindChillF(20, 15); !ok || math.Abs(wc-6.2) > 0.5 {
		t.Errorf("wind chill = %v %v", wc, ok)
	}
	if fp := wx.FrostpointF(30, 20); fp < 20 || fp > 23 {
		t.Errorf("frostpoint = %v", fp)
	}
}

func TestPressureReduction(t *testing.T) {
	t.Parallel()
	if got := wx.AltimeterInHg(28.0, 1000); math.Abs(got-29.04) > 0.01 {
		t.Errorf("altimeter = %v", got)
	}
	if got := wx.AltimeterInHg(24.692, 5431); math.Abs(got-30.153) > 0.01 {
		t.Errorf("altimeter = %v", got)
	}
	if got := wx.SeaLevelInHg(28.0, 0, 50); got != 28.0 {
		t.Errorf("sea level at 0 ft = %v", got)
	}
	v := map[string]float64{wx.Pressure: 28.71, wx.OutTemp: 95}
	wx.DerivePressure(v, 1191)
	if v[wx.Barometer] < 29.85 || v[wx.Barometer] > 30.05 || v[wx.Altimeter] < 29.95 || v[wx.Altimeter] > 30.05 {
		t.Errorf("derived %v", v)
	}
	cold := map[string]float64{wx.Pressure: 28.71, wx.OutTemp: 10}
	wx.DerivePressure(cold, 1191)
	if cold[wx.Barometer] <= v[wx.Barometer] {
		t.Errorf("colder air should reduce to a higher sea level pressure: %v vs %v", cold[wx.Barometer], v[wx.Barometer])
	}
	none := map[string]float64{wx.Pressure: 28.71}
	wx.DerivePressure(none, 1191)
	if none[wx.Barometer] != none[wx.Altimeter] {
		t.Errorf("without temperature barometer should fall back to altimeter: %v", none)
	}
	empty := map[string]float64{}
	wx.DerivePressure(empty, 1191)
	if len(empty) != 0 {
		t.Error("nothing to derive")
	}
}

func TestPressureReducerCarriesTemperature(t *testing.T) {
	t.Parallel()
	r := wx.PressureReducer{ElevFt: 1191}
	first := map[string]float64{wx.Pressure: 28.71, wx.OutTemp: 40}
	r.Apply(1000, first)
	carried := map[string]float64{wx.Pressure: 28.71}
	r.Apply(1000+3600, carried)
	if carried[wx.Barometer] != first[wx.Barometer] {
		t.Errorf("carried %v, want %v", carried[wx.Barometer], first[wx.Barometer])
	}
	stale := map[string]float64{wx.Pressure: 28.71}
	r.Apply(1000+7*3600, stale)
	if stale[wx.Barometer] != stale[wx.Altimeter] {
		t.Errorf("stale temperature should fall back to altimeter: %v", stale)
	}
}

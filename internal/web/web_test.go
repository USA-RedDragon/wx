package web_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/USA-RedDragon/configulator/v2"
	"github.com/USA-RedDragon/wx/internal/config"
	"github.com/USA-RedDragon/wx/internal/store"
	"github.com/USA-RedDragon/wx/internal/web"
	"github.com/USA-RedDragon/wx/internal/wx"
	_ "modernc.org/sqlite"
)

func setup(t *testing.T) *httptest.Server {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Station.Location, cfg.Station.Latitude, cfg.Station.Longitude, cfg.Station.AltitudeFeet = "Test Station", 35.3, -97.5, 1191
	cfg.Station.Timezone, _ = time.LoadLocation("America/Chicago")
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "wx.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	end := time.Date(2024, 11, 26, 8, 0, 0, 0, time.UTC).Unix()
	for i := range 3000 {
		ts := end - int64(3000-i)*60
		r := store.Record{DateTime: ts, Interval: 1, Values: map[string]float64{
			wx.OutTemp: 40 + float64(i%60)/6, wx.OutHumidity: 80, wx.Dewpoint: 35, wx.WindSpeed: 3, wx.WindDir: float64(i % 360),
			wx.WindGust: 5, wx.Rain: 0.01, wx.InTemp: 68, wx.Pressure: 28.7, wx.Barometer: 29.9 + float64(i)/10000, wx.InHumidity: 40, wx.PM25: 12, wx.OutRSSI: -3, wx.OutTempBattery: 0,
		}}
		if err := st.AddLive(ctx, r, cfg.Station.Timezone); err != nil {
			t.Fatal(err)
		}
	}
	srv, err := web.New(&cfg, st, nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func get(t *testing.T, url string) (int, string, string) {
	t.Helper()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header.Get("Content-Type"), string(b)
}

func TestPages(t *testing.T) {
	t.Parallel()
	ts := setup(t)
	code, ctype, body := get(t, ts.URL+"/")
	if code != http.StatusOK || !strings.HasPrefix(ctype, "text/html") {
		t.Fatalf("index %d %s", code, ctype)
	}
	for _, want := range []string{"<title>Test Station</title>", "Barometer", "30.200 inHg (0.018)", "Current Conditions", "Outside Temperature", "Max Wind", "daytempdew.svg", "Weather Station Battery Status", "<option value=\"2024-11\">", "Celestial", "status_ok"} {
		if !strings.Contains(body, want) {
			t.Errorf("index missing %q", want)
		}
	}
	for _, p := range []string{"/daytempdew.svg", "/weekwind.svg", "/monthrain.svg", "/yearwinddir.svg", "/daywindvec.svg", "/daywindrose.svg"} {
		code, ctype, body := get(t, ts.URL+p)
		if code != http.StatusOK || ctype != "image/svg+xml" || !strings.HasPrefix(body, "<svg") {
			t.Errorf("%s: %d %s", p, code, ctype)
		}
	}
	for _, p := range []string{"/seasons.css", "/seasons.js", "/favicon.ico", "/font/OpenSans.woff2", "/healthz"} {
		if code, _, _ := get(t, ts.URL+p); code != http.StatusOK {
			t.Errorf("%s: %d", p, code)
		}
	}
	pages := map[string][]string{
		"/statistics.html":       {"Test Station Statistics", "statistics_widget", "Outside Temperature", "Max Wind", "identifier_widget"},
		"/telemetry.html":        {"Telemetry:", "Weather Station RSSI", "dayoutRSSI.svg"},
		"/celestial.html":        {"Start civil twilight", "Total daylight", "Right ascension", "Solstice", "Equinox", "Full moon", "% full"},
		"/tabular.html":          {"load_file('report', 'report')", "<option value=\"2024\">"},
		"/NOAA/NOAA-2024-11.txt": {"MONTHLY CLIMATOLOGICAL SUMMARY for Nov 2024", "NAME: Test Station", "ELEV: 1191 feet    LAT: 35-18.00 N    LONG: 097-30.00 W", " 24 ", " 30\n"},
		"/NOAA/NOAA-2024.txt":    {"CLIMATOLOGICAL SUMMARY for year 2024", "2024 11 ", "2024 12\n", "PRECIPITATION (in)", "WIND SPEED (mph)"},
	}
	for p, wants := range pages {
		code, _, body := get(t, ts.URL+p)
		if code != http.StatusOK {
			t.Errorf("%s: %d", p, code)
			continue
		}
		for _, want := range wants {
			if !strings.Contains(body, want) {
				t.Errorf("%s missing %q", p, want)
			}
		}
	}
	for _, p := range []string{"/NOAA/NOAA-2024-13.txt", "/NOAA/bogus.txt"} {
		if code, _, _ := get(t, ts.URL+p); code != http.StatusNotFound {
			t.Errorf("%s: %d", p, code)
		}
	}
	if code, _, _ := get(t, ts.URL+"/daybogus.svg"); code != http.StatusNotFound {
		t.Errorf("unknown plot: %d", code)
	}
}

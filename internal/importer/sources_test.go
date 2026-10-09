package importer_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/USA-RedDragon/wx/internal/importer"
	"github.com/USA-RedDragon/wx/internal/store"
	"github.com/USA-RedDragon/wx/internal/wx"
	"github.com/coder/websocket"
	_ "modernc.org/sqlite"
)

const elevation = 1191

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "wx.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func writeWeewx(t *testing.T, start int64, n int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "weewx.sdb")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(context.Background(), `CREATE TABLE archive ("dateTime" INTEGER PRIMARY KEY, "usUnits" INTEGER NOT NULL, "interval" INTEGER NOT NULL, "outTemp" REAL, "windSpeed" REAL, "windDir" REAL, "windGust" REAL, "rain" REAL, "lightning_strike_count" REAL, "extraTemp1" REAL)`); err != nil {
		t.Fatal(err)
	}
	for i := range n {
		if _, err := db.ExecContext(context.Background(), `INSERT INTO archive VALUES (?, 1, 0, ?, 2, 90, ?, 0.001, 42, 1)`, start+int64(i*2), 50+float64(i%30)/10, float64(i%10)); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func TestImportWeewxSQLite(t *testing.T) {
	t.Parallel()
	st := openStore(t)
	ctx := context.Background()
	start := int64(1730332080)
	res, err := importer.ImportWeewxSQLite(ctx, st, writeWeewx(t, start, 300), 60, elevation)
	if err != nil {
		t.Fatal(err)
	}
	if res.Read != 300 || res.Inserted != 11 {
		t.Fatalf("result %+v", res)
	}
	recs, err := st.Range(ctx, 0, start+3600)
	if err != nil {
		t.Fatal(err)
	}
	r := recs[1]
	if r.Source != store.SourceWeewx || r.Interval != 1 || r.Values[wx.WindGust] != 9 || r.Values[wx.Rain] < 0.0299 || r.Values[wx.Rain] > 0.0301 {
		t.Errorf("record %+v", r)
	}
	if _, ok := r.Values[wx.LightningStrikeCount]; ok {
		t.Error("cumulative lightning counter should not be imported")
	}
	again, err := importer.ImportWeewxSQLite(ctx, st, writeWeewx(t, start, 300), 60, elevation)
	if err != nil || again.Inserted != 0 {
		t.Errorf("reimport inserted %d (%v)", again.Inserted, err)
	}
	bad := filepath.Join(t.TempDir(), "bad.sdb")
	db, _ := sql.Open("sqlite", bad)
	_, _ = db.ExecContext(ctx, `CREATE TABLE other (x INTEGER)`)
	_ = db.Close()
	if _, err := importer.ImportWeewxSQLite(ctx, st, bad, 60, elevation); err == nil {
		t.Error("expected error for a database without an archive table")
	}
}

func TestPrometheusRecordsAndGapFill(t *testing.T) {
	t.Parallel()
	base := int64(1789682460)
	series := importer.Series{}
	for i := range 120 {
		ts := float64(base + int64(i*60))
		series["outside_temperature"] = append(series["outside_temperature"], [2]float64{ts, 30 + float64(i%7)/10})
		series["outside_humidity"] = append(series["outside_humidity"], [2]float64{ts, 40})
		series["indoor_sensors_outside_pressure"] = append(series["indoor_sensors_outside_pressure"], [2]float64{ts, 14.1})
		series["total_precipitation"] = append(series["total_precipitation"], [2]float64{ts, float64(i/60) * 0.1})
		series["living_room_temperature"] = append(series["living_room_temperature"], [2]float64{ts, 20})
		series["indoor_sensors_living_room_temperature"] = append(series["indoor_sensors_living_room_temperature"], [2]float64{ts, 22 + float64(i%3)/10})
	}
	recs := importer.PrometheusRecords(series, 60, elevation)
	if len(recs) != 120 {
		t.Fatalf("got %d records", len(recs))
	}
	r := recs[61]
	if r.Values[wx.Barometer] < 29.9 || r.Values[wx.Pressure] < 28.7 || r.Values[wx.InTemp] < 71.6 || r.Values[wx.Dewpoint] == 0 {
		t.Errorf("record %+v", r.Values)
	}
	var rain float64
	for _, r := range recs {
		rain += r.Values[wx.Rain]
	}
	if rain < 0.0999 || rain > 0.1001 {
		t.Errorf("rain %v", rain)
	}
	st := openStore(t)
	ctx := context.Background()
	if _, err := st.InsertRecords(ctx, recs[:10], false); err != nil {
		t.Fatal(err)
	}
	res, err := importer.ImportRecordsGapFill(ctx, st, recs, "test")
	if err != nil || res.Inserted != 110 || res.First != recs[10].DateTime {
		t.Errorf("gap fill %+v %v", res, err)
	}
}

func TestLoadFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "outside_temperature.tsv"), []byte("1789682460\t30.5\nbad line\n1789682520\t31\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := importer.LoadPrometheusDir(dir)
	if err != nil || len(s["outside_temperature"]) != 2 {
		t.Errorf("series %v %v", s, err)
	}
	stats := map[string][]importer.HAStat{"sensor.outside_temperature": {{End: 3600000, Mean: f(70)}}}
	b, err := json.Marshal(stats)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "hour.json")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := importer.LoadHAStats([]string{p, p})
	if err != nil || len(loaded["sensor.outside_temperature"]) != 2 {
		t.Errorf("stats %v %v", loaded, err)
	}
	if err := os.WriteFile(p, []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := importer.LoadHAStats([]string{p}); err == nil {
		t.Error("expected decode error")
	}
}

func TestFetchPrometheus(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("query")
		if !strings.Contains(q, `entity="sensor.outside_temperature"`) {
			_, _ = w.Write([]byte(`{"status":"success","data":{"result":[]}}`))
			return
		}
		start, err := strconv.ParseInt(r.URL.Query().Get("start"), 10, 64)
		if err != nil {
			http.Error(w, "bad start", http.StatusBadRequest)
			return
		}
		_, _ = fmt.Fprintf(w, `{"status":"success","data":{"result":[{"values":[[%d,"30.5"],[%d,"bad"]]}]}}`, start, start)
	}))
	defer srv.Close()
	start := time.Unix(1789682460, 0)
	s, err := importer.FetchPrometheus(context.Background(), srv.URL, start, start.Add(10*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(s["outside_temperature"]) != 2 || s["outside_temperature"][0][1] != 30.5 {
		t.Errorf("series %v", s["outside_temperature"])
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"error","error":"boom"}`))
	}))
	defer bad.Close()
	if _, err := importer.FetchPrometheus(context.Background(), bad.URL, start, start.Add(time.Hour)); err == nil {
		t.Error("expected error")
	}
}

func haServer(t *testing.T, token string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = c.CloseNow() }()
		ctx := r.Context()
		_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"auth_required"}`))
		_, b, err := c.Read(ctx)
		if err != nil {
			return
		}
		var auth map[string]string
		_ = json.Unmarshal(b, &auth)
		if auth["access_token"] != token {
			_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"auth_invalid"}`))
			return
		}
		_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"auth_ok"}`))
		for {
			_, b, err := c.Read(ctx)
			if err != nil {
				return
			}
			var req struct {
				ID        int    `json:"id"`
				StartTime string `json:"start_time"`
			}
			_ = json.Unmarshal(b, &req)
			st, _ := time.Parse(time.RFC3339, req.StartTime)
			ms := st.Add(time.Hour).UnixMilli()
			_ = c.Write(ctx, websocket.MessageText, []byte(`{"id":0,"type":"event"}`))
			_ = c.Write(ctx, websocket.MessageText, fmt.Appendf(nil, `{"id":%d,"type":"result","success":true,"result":{"sensor.outside_temperature":[{"start":%d,"end":%d,"mean":70,"min":65,"max":75}]}}`, req.ID, ms-3600000, ms))
		}
	}))
}

func TestFetchHAStatistics(t *testing.T) {
	t.Parallel()
	srv := haServer(t, "good")
	defer srv.Close()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	stats, err := importer.FetchHAStatistics(context.Background(), srv.URL, "good", start, start.Add(40*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(stats["sensor.outside_temperature"]) != 2 {
		t.Errorf("stats %v", stats)
	}
	if _, err := importer.FetchHAStatistics(context.Background(), srv.URL, "bad", start, start.Add(time.Hour)); err == nil {
		t.Error("expected auth error")
	}
	if _, err := importer.FetchHAStatistics(context.Background(), srv.URL, "", start, start.Add(time.Hour)); err == nil {
		t.Error("expected missing token error")
	}
	if ids := importer.HAStatisticIDs(); len(ids) < 20 || ids[0] != "sensor.total_precipitation" {
		t.Errorf("ids %v", ids)
	}
}

func TestImportWxDatabase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	seedPath := filepath.Join(dir, "seed.db")
	seed, err := store.Open(context.Background(), seedPath)
	if err != nil {
		t.Fatal(err)
	}
	recs := []store.Record{
		{DateTime: 3600, Interval: 60, Source: store.SourceHAHourly, Values: map[string]float64{wx.OutTemp: 50}},
		{DateTime: 3660, Interval: 1, Source: store.SourceWeewx, Values: map[string]float64{wx.OutTemp: 51}},
	}
	if _, err := seed.InsertRecords(ctx, recs, false); err != nil {
		t.Fatal(err)
	}
	_ = seed.Close()
	st := openStore(t)
	if _, err := st.InsertRecords(ctx, []store.Record{{DateTime: 3660, Interval: 1, Values: map[string]float64{wx.OutTemp: 52}}}, false); err != nil {
		t.Fatal(err)
	}
	res, err := importer.ImportWxDatabase(ctx, st, seedPath)
	if err != nil || res.Inserted != 1 {
		t.Fatalf("result %+v %v", res, err)
	}
	got, err := st.Range(ctx, 0, 4000)
	if err != nil || len(got) != 2 || got[0].Source != store.SourceHAHourly || got[0].Interval != 60 || got[1].Values[wx.OutTemp] != 52 {
		t.Errorf("records %+v %v", got, err)
	}
}

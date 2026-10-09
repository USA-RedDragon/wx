package importer

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/USA-RedDragon/wx/internal/archive"
	"github.com/USA-RedDragon/wx/internal/store"
	"github.com/USA-RedDragon/wx/internal/wx"
)

type promSeries struct {
	Obs     string
	Metric  string
	Prio    int
	Convert func(float64) float64
}

const (
	promRainEntity = "total_precipitation"
	promStep       = 60
	promChunk      = 7 * 24 * time.Hour
	promTimeout    = 60 * time.Second
	metricTemp     = "home_assistant_sensor_temperature_celsius"
	metricHumidity = "home_assistant_sensor_humidity_percent"
	metricWind     = "home_assistant_sensor_wind_speed_mph"
	metricCO2      = "home_assistant_sensor_carbon_dioxide_ppm"
)

func promEntities() map[string]promSeries {
	return map[string]promSeries{
		"outside_temperature":                       {wx.OutTemp, metricTemp, 0, wx.CToF},
		"outside_humidity":                          {wx.OutHumidity, metricHumidity, 0, identity},
		"average_wind":                              {wx.WindSpeed, metricWind, 0, identity},
		"peak_wind":                                 {wx.WindGust, metricWind, 0, identity},
		"wind_direction":                            {wx.WindDir, "home_assistant_sensor_unit_u0xb0", 0, identity},
		"uv_index":                                  {wx.UV, "home_assistant_sensor_state", 0, identity},
		"indoor_sensors_living_room_temperature":    {wx.InTemp, metricTemp, 0, wx.CToF},
		"living_room_temperature":                   {wx.InTemp, metricTemp, 1, wx.CToF},
		"indoor_sensors_living_room_humidity":       {wx.InHumidity, metricHumidity, 0, identity},
		"indoor_sensors_outside_pressure":           {wx.Pressure, "home_assistant_sensor_pressure_psi", 0, wx.PsiToInHg},
		"sensy_living_room_scd40_co2_concentration": {wx.CO2, metricCO2, 0, identity},
		"indoor_sensors_ccs811_eco2":                {wx.CO2, metricCO2, 1, identity},
		"indoor_sensors_ccs811_tvoc":                {wx.TVOC, "home_assistant_sensor_volatile_organic_compounds_parts_ppb", 0, func(v float64) float64 { return v * 0.001 }},
		"particulate_sensor_dsm501a_pm_2_5um_3":     {wx.PM25, "home_assistant_sensor_pm25_u0xb5g_per_mu0xb3", 0, identity},
		"particulate_sensor_dsm501a_pm_1_0um_3":     {wx.PM1, "home_assistant_sensor_pm1_u0xb5g_per_mu0xb3", 0, identity},
		promRainEntity:                              {wx.Rain, "home_assistant_sensor_precipitation_in", 0, identity},
	}
}

type Series map[string][][2]float64

func readTSV(path string) ([][2]float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out [][2]float64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		parts := strings.Split(sc.Text(), "\t")
		if len(parts) != 2 {
			continue
		}
		ts, err1 := strconv.ParseFloat(parts[0], 64)
		v, err2 := strconv.ParseFloat(parts[1], 64)
		if err1 != nil || err2 != nil {
			continue
		}
		out = append(out, [2]float64{ts, v})
	}
	return out, sc.Err()
}

func LoadPrometheusDir(dir string) (Series, error) {
	out := Series{}
	for name := range promEntities() {
		rows, err := readTSV(filepath.Join(dir, name+".tsv"))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		out[name] = rows
	}
	return out, nil
}

type promResponse struct {
	Status string `json:"status"`
	Error  string `json:"error"`
	Data   struct {
		Result []struct {
			Values [][2]any `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

func FetchPrometheus(ctx context.Context, base string, start, end time.Time) (Series, error) {
	client := &http.Client{Timeout: promTimeout}
	out := Series{}
	for name, ps := range promEntities() {
		query := fmt.Sprintf(`%s{entity="sensor.%s"}`, ps.Metric, name)
		for from := start; from.Before(end); from = from.Add(promChunk) {
			to := from.Add(promChunk)
			if to.After(end) {
				to = end
			}
			q := url.Values{}
			q.Set("query", query)
			q.Set("start", strconv.FormatInt(from.Unix(), 10))
			q.Set("end", strconv.FormatInt(to.Unix(), 10))
			q.Set("step", strconv.Itoa(promStep))
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(base, "/")+"/api/v1/query_range?"+q.Encode(), nil)
			if err != nil {
				return nil, err
			}
			resp, err := client.Do(req)
			if err != nil {
				return nil, err
			}
			var pr promResponse
			err = json.NewDecoder(resp.Body).Decode(&pr)
			_ = resp.Body.Close()
			if err != nil {
				return nil, fmt.Errorf("decode %s: %w", name, err)
			}
			if pr.Status != "success" {
				return nil, fmt.Errorf("query %s: %s", name, pr.Error)
			}
			for _, r := range pr.Data.Result {
				for _, v := range r.Values {
					ts, ok1 := v[0].(float64)
					s, ok2 := v[1].(string)
					if !ok1 || !ok2 {
						continue
					}
					f, err := strconv.ParseFloat(s, 64)
					if err != nil {
						continue
					}
					out[name] = append(out[name], [2]float64{ts, f})
				}
			}
		}
	}
	return out, nil
}

func PrometheusRecords(series Series, intervalSecs int64, elevFt float64) []store.Record {
	type point struct {
		vals map[string]float64
		prio map[string]int
	}
	points := map[int64]*point{}
	get := func(ts int64) *point {
		p, ok := points[ts]
		if !ok {
			p = &point{vals: map[string]float64{}, prio: map[string]int{}}
			points[ts] = p
		}
		return p
	}
	for name, ps := range promEntities() {
		rows := series[name]
		if name == promRainEntity {
			sort.Slice(rows, func(i, j int) bool { return rows[i][0] < rows[j][0] })
			var tracker archive.RainTracker
			for _, r := range rows {
				get(int64(r[0])).vals[wx.Rain] = tracker.Update(r[1])
			}
			continue
		}
		for _, r := range rows {
			p := get(int64(r[0]))
			if cur, ok := p.prio[ps.Obs]; ok && cur <= ps.Prio {
				continue
			}
			p.prio[ps.Obs] = ps.Prio
			p.vals[ps.Obs] = ps.Convert(r[1])
		}
	}
	keys := make([]int64, 0, len(points))
	for k := range points {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	vals := make([]map[string]float64, len(keys))
	for i, k := range keys {
		vals[i] = points[k].vals
	}
	DropStale(keys, vals, wx.OutTemp, outdoorObs(), staleOutdoor)
	DropStale(keys, vals, wx.InTemp, indoorObs(), staleIndoor)
	b := NewBucketer(intervalSecs, store.SourcePrometheus)
	reducer := &wx.PressureReducer{ElevFt: elevFt}
	for i, k := range keys {
		v := vals[i]
		derive(k, v, reducer)
		if r, ok := v[wx.Rain]; ok {
			v[wx.RainRate] = r * 3600 / promStep
		}
		b.Add(k, v)
	}
	return b.Drain(true)
}

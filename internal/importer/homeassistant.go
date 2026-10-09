package importer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"sort"

	"github.com/USA-RedDragon/wx/internal/store"
	"github.com/USA-RedDragon/wx/internal/wx"
)

type HAStat struct {
	Start  int64    `json:"start"`
	End    int64    `json:"end"`
	Mean   *float64 `json:"mean"`
	Min    *float64 `json:"min"`
	Max    *float64 `json:"max"`
	State  *float64 `json:"state"`
	Sum    *float64 `json:"sum"`
	Change *float64 `json:"change"`
}

type entityMap struct {
	Obs      string
	Entities []string
	Convert  func(float64) float64
}

func identity(v float64) float64 { return v }

func haEntities() []entityMap {
	return []entityMap{
		{wx.OutTemp, []string{"sensor.outside_temperature"}, identity},
		{wx.OutHumidity, []string{"sensor.outside_humidity"}, identity},
		{wx.WindSpeed, []string{"sensor.average_wind"}, identity},
		{wx.WindGust, []string{"sensor.peak_wind"}, identity},
		{wx.WindDir, []string{"sensor.wind_direction"}, identity},
		{wx.UV, []string{"sensor.uv_index"}, identity},
		{wx.Dewpoint, []string{"sensor.dewpoint"}, identity},
		{wx.HeatIndex, []string{"sensor.heat_index"}, identity},
		{wx.WindChill, []string{"sensor.wind_chill"}, identity},
		{wx.InTemp, []string{"sensor.indoor_sensors_living_room_temperature", "sensor.living_room_temperature", "sensor.living_room_temperature_3", "sensor.living_room_temperature_2", "sensor.indoor_temp_humidity_living_room_temperature", "sensor.gas_sensor_living_room_temperature"}, identity},
		{wx.InHumidity, []string{"sensor.indoor_sensors_living_room_humidity", "sensor.living_room_humidity", "sensor.living_room_humidity_3", "sensor.living_room_humidity_2", "sensor.indoor_temp_humidity_living_room_humidity", "sensor.gas_sensor_living_room_humidity"}, identity},
		{wx.Pressure, []string{"sensor.indoor_sensors_outside_pressure", "sensor.outside_pressure", "sensor.weather_node_outside_pressure", "sensor.lightning_outside_pressure"}, identity},
		{wx.Luminosity, []string{"sensor.outside_lux", "sensor.weather_node_outside_lux", "sensor.lightning_outside_lux"}, identity},
		{wx.CO2, []string{"sensor.sensy_living_room_scd40_co2_concentration", "sensor.indoor_sensors_ccs811_eco2", "sensor.indoor_sensors_sgp30_eco2"}, identity},
		{wx.TVOC, []string{"sensor.indoor_sensors_ccs811_tvoc", "sensor.indoor_sensors_sgp30_tvoc"}, func(v float64) float64 { return v * 0.001 }},
		{wx.PM25, []string{"sensor.particulate_sensor_dsm501a_pm_2_5um_3", "sensor.particulate_sensor_dsm501a_pm_2_5um_2"}, identity},
		{wx.PM1, []string{"sensor.particulate_sensor_dsm501a_pm_1_0um_3", "sensor.particulate_sensor_dsm501a_pm_1_0um_2"}, identity},
	}
}

const (
	haRainEntity   = "sensor.total_precipitation"
	maxHourlyRain  = 3.0
	maxDirSpreadHA = 180.0
)

func LoadHAStats(paths []string) (map[string][]HAStat, error) {
	out := map[string][]HAStat{}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		m := map[string][]HAStat{}
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		for k, v := range m {
			out[k] = append(out[k], v...)
		}
	}
	return out, nil
}

func HAHourRecords(stats map[string][]HAStat, elevFt float64) []store.Record {
	type slot struct {
		vals map[string]float64
		hi   map[string]store.Extreme
		lo   map[string]store.Extreme
		prio map[string]int
	}
	slots := map[int64]*slot{}
	get := func(end int64) *slot {
		s, ok := slots[end]
		if !ok {
			s = &slot{vals: map[string]float64{}, hi: map[string]store.Extreme{}, lo: map[string]store.Extreme{}, prio: map[string]int{}}
			slots[end] = s
		}
		return s
	}
	for _, em := range haEntities() {
		for prio, ent := range em.Entities {
			for _, h := range stats[ent] {
				if h.Mean == nil {
					continue
				}
				end := h.End / 1000
				s := get(end)
				if p, ok := s.prio[em.Obs]; ok && p <= prio {
					continue
				}
				if em.Obs == wx.WindDir && h.Min != nil && h.Max != nil && *h.Max-*h.Min > maxDirSpreadHA {
					continue
				}
				s.prio[em.Obs] = prio
				mean := em.Convert(*h.Mean)
				if em.Obs == wx.WindGust && h.Max != nil {
					mean = em.Convert(*h.Max)
				}
				s.vals[em.Obs] = mean
				if h.Max != nil {
					s.hi[em.Obs] = store.Extreme{Value: em.Convert(*h.Max), Time: end - 1800}
				}
				if h.Min != nil {
					s.lo[em.Obs] = store.Extreme{Value: em.Convert(*h.Min), Time: end - 1800}
				}
			}
		}
	}
	for _, h := range stats[haRainEntity] {
		if h.Change == nil || *h.Change < 0 || *h.Change > maxHourlyRain {
			continue
		}
		s := get(h.End / 1000)
		s.vals[wx.Rain] = *h.Change
		s.vals[wx.RainRate] = *h.Change
	}
	ends := make([]int64, 0, len(slots))
	for k := range slots {
		ends = append(ends, k)
	}
	sort.Slice(ends, func(i, j int) bool { return ends[i] < ends[j] })
	out := make([]store.Record, 0, len(ends))
	reducer := &wx.PressureReducer{ElevFt: elevFt}
	for _, end := range ends {
		s := slots[end]
		derive(end, s.vals, reducer)
		if len(s.vals) == 0 {
			continue
		}
		out = append(out, store.Record{DateTime: end, Interval: 60, Source: store.SourceHAHourly, Values: s.vals, Hi: s.hi, Lo: s.lo})
	}
	return out
}

func derive(ts int64, v map[string]float64, reducer *wx.PressureReducer) {
	t, okT := v[wx.OutTemp]
	rh, okH := v[wx.OutHumidity]
	if okT && okH {
		if _, ok := v[wx.Dewpoint]; !ok {
			if d := wx.DewpointF(t, rh); !math.IsNaN(d) {
				v[wx.Dewpoint] = d
			}
		}
	}
	if d, ok := v[wx.Dewpoint]; ok && okT {
		v[wx.Cloudbase] = wx.MToFeet((wx.FToC(t)-wx.FToC(d))/2.4*1000 + 2.7432 + 363.2)
		if _, ok := v[wx.Frostpoint]; !ok {
			v[wx.Frostpoint] = wx.FrostpointF(t, d)
		}
	}
	if okT && okH {
		if _, ok := v[wx.HeatIndex]; !ok {
			v[wx.HeatIndex] = wx.HeatIndexF(t, rh)
		}
	}
	if s, ok := v[wx.WindSpeed]; ok && okT {
		if _, ok := v[wx.WindChill]; !ok {
			if wc, valid := wx.WindChillF(t, s); valid {
				v[wx.WindChill] = wc
			}
		}
	}
	reducer.Apply(ts, v)
}

func ImportRecordsGapFill(ctx context.Context, st *store.Store, recs []store.Record, name string) (Result, error) {
	res := Result{Source: name}
	var batch []store.Record
	flush := func() error {
		n, err := st.InsertRecords(ctx, batch, false)
		res.Inserted += n
		batch = batch[:0]
		return err
	}
	for _, r := range recs {
		res.Read++
		covered, err := st.Covered(ctx, r.DateTime-int64(r.Interval)*60, r.DateTime)
		if err != nil {
			return res, err
		}
		if covered {
			continue
		}
		if res.First == 0 || r.DateTime < res.First {
			res.First = r.DateTime
		}
		if r.DateTime > res.Last {
			res.Last = r.DateTime
		}
		batch = append(batch, r)
		if len(batch) >= 5000 {
			if err := flush(); err != nil {
				return res, err
			}
		}
	}
	if err := flush(); err != nil {
		return res, err
	}
	slog.Info("imported records", "result", res.String())
	return res, nil
}

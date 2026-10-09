package web

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/USA-RedDragon/wx/internal/almanac"
	"github.com/USA-RedDragon/wx/internal/plot"
	"github.com/USA-RedDragon/wx/internal/store"
	"github.com/USA-RedDragon/wx/internal/wx"
)

const (
	fmtDay        = "03:04:05 PM MST"
	fmtWeek       = "03:04:05 PM MST on Monday"
	fmtLong       = "02 Jan 2006 03:04:05 PM MST"
	recentDays    = 30
	trendSeconds  = 3 * 3600
	trendGrace    = 300
	kindSum       = "sum"
	kindMax       = "max"
	kindMinMax    = "minmax"
	kindTwo       = "two"
	kindOne       = "one"
	obsWindSpeed  = wx.WindSpeed
	obsWindDir    = wx.WindDir
	obsBarometer  = wx.Barometer
	obsRain       = wx.Rain
	statusOK      = "ok"
	statusLow     = "low"
	statusUnknown = "unknown"
	unitMph       = "mph"
	unitDeg       = "°"
)

func observationsCurrent() []string {
	return []string{wx.OutTemp, wx.HeatIndex, wx.WindChill, wx.Dewpoint, wx.Frostpoint, wx.OutHumidity, obsBarometer, obsWindSpeed, obsRain, wx.RainRate, wx.UV, wx.Radiation, wx.Cloudbase, wx.LightningStrikeCount, wx.InTemp, wx.InHumidity, wx.PM1, wx.PM25}
}

func observationsStats() []string {
	return []string{wx.OutTemp, wx.HeatIndex, wx.WindChill, wx.Dewpoint, wx.Frostpoint, wx.OutHumidity, obsBarometer, obsWindSpeed, obsRain, wx.RainRate, "ET", wx.UV, wx.Radiation, wx.LightningStrikeCount, wx.LightningDistance, wx.InTemp, wx.InHumidity, wx.PM1, wx.PM25, wx.CO2}
}

func obsKind(name string) string {
	switch name {
	case obsRain, "ET", wx.LightningStrikeCount:
		return kindSum
	case wx.RainRate, wx.UV:
		return kindMax
	}
	return kindMinMax
}

func sensorConnections() []string {
	return []string{"inRSSI", "inSNR", "inNoise", wx.OutRSSI, wx.OutSNR, wx.OutNoise, "rxCheckPercent"}
}

func sensorBatteries() []string {
	return []string{wx.OutTempBattery, "inTempBatteryStatus", "rainBatteryStatus", "windBatteryStatus", "uvBatteryStatus", "txBatteryStatus"}
}

func telemetryGroups() []string {
	return []string{"inRSSI", "inSNR", "inNoise", wx.OutRSSI, wx.OutSNR, wx.OutNoise, "rx", "volt"}
}

type Row struct {
	Label string
	Value string
}

type HiloCell struct {
	Span     string
	Max      string
	MaxTitle string
	Min      string
	MinTitle string
	Second   string
}

type HiloRow struct {
	Label  string
	Kind   string
	Cells  []HiloCell
	Units  string
	Units2 string
}

type SensorRow struct {
	Label  string
	Value  string
	Status string
	Ago    string
}

type Sensors struct {
	Connectivity []SensorRow
	Batteries    []SensorRow
}

type Celestial struct {
	SunRise, SunSet, MoonRise, MoonSet, Daylight, MoonPhase string
	MoonFullness                                            int
}

type CelestialDetail struct {
	CivilDawn, SunRise, SunTransit, SunSet, CivilDusk string
	SunAz, SunAlt, SunRA, SunDec                      string
	FirstSeason, SecondSeason                         Row
	Daylight, DaylightChange                          string
	MoonRise, MoonTransit, MoonSet                    string
	MoonAz, MoonAlt, MoonRA, MoonDec                  string
	FirstPhase, SecondPhase                           Row
	MoonPhase                                         string
	MoonFullness                                      int
}

type PeriodPlots struct {
	Name  string
	Plots []string
}

type Header struct {
	Location   string
	LastUpdate string
	Nocache    int64
	Months     []string
	Years      []string
	Identifier []Row
}

type IndexPage struct {
	Header
	Current    []Row
	Hilo       []HiloRow
	Sensors    Sensors
	Celestial  Celestial
	About      []Row
	Periods    []PeriodPlots
	RadarImage string
	RadarURL   string
	SatImage   string
	SatURL     string
	Forecast   *Forecast
}

type StatisticsPage struct {
	Header
	Rows []HiloRow
}

type TelemetryPage struct {
	Header
	Sensors Sensors
	Periods []PeriodPlots
}

type CelestialPage struct {
	Header
	Detail CelestialDetail
}

type span struct {
	name       string
	start, end int64
	fmt        string
}

type pageCtx struct {
	latest    store.Record
	ref       time.Time
	spans     []span
	recentDay int64
	yearStart int64
}

func (s *Server) spans(ref time.Time) []span {
	loc := s.cfg.Station.Timezone
	day := time.Date(ref.Year(), ref.Month(), ref.Day(), 0, 0, 0, 0, loc)
	weekStart := (s.cfg.Station.WeekStart + 1) % 7
	wd := (int(day.Weekday()) - weekStart + 7) % 7
	week := day.AddDate(0, 0, -wd)
	month := time.Date(ref.Year(), ref.Month(), 1, 0, 0, 0, 0, loc)
	year := time.Date(ref.Year(), 1, 1, 0, 0, 0, 0, loc)
	ry := time.Date(ref.Year(), time.Month(s.cfg.Station.RainYearStart), 1, 0, 0, 0, 0, loc)
	if ry.After(ref) {
		ry = ry.AddDate(-1, 0, 0)
	}
	end := day.AddDate(0, 0, 1).Unix()
	return []span{
		{plot.PeriodDay, day.Unix(), end, fmtDay},
		{plot.PeriodWeek, week.Unix(), end, fmtWeek},
		{plot.PeriodMonth, month.Unix(), end, fmtLong},
		{plot.PeriodYear, year.Unix(), end, fmtLong},
		{"rainyear", ry.Unix(), end, fmtLong},
	}
}

func (s *Server) context(ctx context.Context) (*pageCtx, error) {
	latest, err := s.store.Latest(ctx)
	if err != nil {
		return nil, err
	}
	loc := s.cfg.Station.Timezone
	ref := time.Unix(latest.DateTime, 0).In(loc)
	pc := &pageCtx{latest: latest, ref: ref, spans: s.spans(ref)}
	pc.recentDay = time.Date(ref.Year(), ref.Month(), ref.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -recentDays).Unix()
	pc.yearStart = pc.spans[3].start
	return pc, nil
}

func (s *Server) fmtTime(ts int64, layout string) string {
	if ts == 0 {
		return ""
	}
	return time.Unix(ts, 0).In(s.cfg.Station.Timezone).Format(layout)
}

func fmtNum(name string, v *float64) string {
	return wx.FormatValue(name, v, false)
}

func (s *Server) header(ctx context.Context, pc *pageCtx) (Header, error) {
	h := Header{
		Location:   s.cfg.Station.Location,
		LastUpdate: pc.ref.Format(fmtLong),
		Nocache:    pc.latest.DateTime,
		Identifier: []Row{
			{"Latitude", dms(s.cfg.Station.Latitude, "N", "S", 2)},
			{"Longitude", dms(s.cfg.Station.Longitude, "E", "W", 3)},
			{"Altitude", fmt.Sprintf("%.0f feet", s.cfg.Station.AltitudeFeet)},
			{"wx", s.version},
		},
	}
	months, err := s.store.Months(ctx, s.cfg.Station.Timezone)
	if err != nil {
		return h, err
	}
	seenYear := map[string]bool{}
	for _, m := range months {
		h.Months = append(h.Months, m.Format("2006-01"))
		y := m.Format("2006")
		if !seenYear[y] {
			seenYear[y] = true
			h.Years = append(h.Years, y)
		}
	}
	return h, nil
}

func (s *Server) trend(ctx context.Context, pc *pageCtx, obs string) string {
	cur, ok := pc.latest.Values[obs]
	if !ok {
		return "N/A"
	}
	then, ok, err := s.store.ValueNear(ctx, obs, pc.latest.DateTime-trendSeconds, trendGrace)
	if err != nil || !ok {
		return "N/A"
	}
	d := cur - then
	return wx.FormatValue(obs, &d, false)
}

func (s *Server) buildIndex(ctx context.Context) (*IndexPage, error) {
	pc, err := s.context(ctx)
	if err != nil {
		return nil, err
	}
	h, err := s.header(ctx, pc)
	if err != nil {
		return nil, err
	}
	p := &IndexPage{
		Header:     h,
		RadarImage: s.cfg.Station.RadarImage,
		RadarURL:   s.cfg.Station.RadarURL,
		SatImage:   s.cfg.Station.SatelliteImg,
		SatURL:     s.cfg.Station.SatelliteURL,
	}
	if s.nws != nil {
		p.Forecast = s.nws.Current()
	}
	if p.Current, err = s.currentRows(ctx, pc); err != nil {
		return nil, err
	}
	if p.Hilo, err = s.hiloRows(ctx, pc); err != nil {
		return nil, err
	}
	if p.Sensors, err = s.sensors(ctx, pc); err != nil {
		return nil, err
	}
	p.Celestial = s.celestial(pc)
	p.About = []Row{
		{"Hardware", "mqtt"},
		{"Latitude", dms(s.cfg.Station.Latitude, "N", "S", 2)},
		{"Longitude", dms(s.cfg.Station.Longitude, "E", "W", 3)},
		{"Altitude", fmt.Sprintf("%.0f feet", s.cfg.Station.AltitudeFeet)},
		{"Server uptime", uptime(time.Since(s.started))},
		{"wx version", s.version},
	}
	if p.Periods, err = s.periodPlots(ctx, plot.Groups(), pc.recentDay); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Server) periodPlots(ctx context.Context, groups []string, since int64) ([]PeriodPlots, error) {
	out := make([]PeriodPlots, 0, len(plot.Periods()))
	for _, per := range plot.Periods() {
		pp := PeriodPlots{Name: per.Name}
		specs := plot.Specs(per.Name)
		for _, g := range groups {
			spec, ok := specs[g]
			if !ok {
				continue
			}
			for _, o := range plot.Obs(spec) {
				has, err := s.store.HasData(ctx, o, since)
				if err != nil {
					return nil, err
				}
				if has {
					pp.Plots = append(pp.Plots, per.Name+g)
					break
				}
			}
		}
		out = append(out, pp)
	}
	return out, nil
}

func (s *Server) currentRows(ctx context.Context, pc *pageCtx) ([]Row, error) {
	var out []Row
	cur := pc.latest
	for _, x := range observationsCurrent() {
		has, err := s.store.HasData(ctx, x, pc.yearStart)
		if err != nil {
			return nil, err
		}
		if !has {
			continue
		}
		switch x {
		case obsBarometer:
			out = append(out, Row{wx.Label(x), fmt.Sprintf("%s (%s)", wx.FormatValue(x, cur.Get(x), true), s.trend(ctx, pc, x))})
		case obsWindSpeed:
			out = append(out, Row{"Wind", fmt.Sprintf("%s %s (%s)", wx.FormatValue(obsWindSpeed, cur.Get(obsWindSpeed), true), wx.Ordinal(cur.Get(obsWindDir)), wx.FormatValue(obsWindDir, cur.Get(obsWindDir), true))})
		case obsRain:
			st, err := s.store.Stats(ctx, obsRain, pc.spans[0].start, pc.spans[0].end)
			if err != nil {
				return nil, err
			}
			out = append(out, Row{"Rain Today", wx.FormatValue(obsRain, orZero(st.Sum), true)})
		default:
			out = append(out, Row{wx.Label(x), wx.FormatValue(x, cur.Get(x), true)})
		}
	}
	return out, nil
}

func (s *Server) hiloRows(ctx context.Context, pc *pageCtx) ([]HiloRow, error) {
	var out []HiloRow
	for _, x := range observationsStats() {
		has, err := s.store.HasData(ctx, x, pc.recentDay)
		if err != nil {
			return nil, err
		}
		if !has {
			continue
		}
		if x == obsWindSpeed {
			rows, err := s.windRows(ctx, pc.spans)
			if err != nil {
				return nil, err
			}
			out = append(out, rows...)
			continue
		}
		row := HiloRow{Label: wx.Label(x), Units: strings.TrimSpace(wx.UnitLabel(x)), Kind: obsKind(x)}
		for _, sp := range pc.spans {
			st, err := s.store.Stats(ctx, x, sp.start, sp.end)
			if err != nil {
				return nil, err
			}
			c := HiloCell{Span: sp.name}
			switch row.Kind {
			case kindSum:
				c.Max = fmtNum(x, orZero(st.Sum))
			default:
				c.Max, c.MaxTitle = fmtNum(x, st.Max), s.fmtTime(st.MaxTime, sp.fmt)
				c.Min, c.MinTitle = fmtNum(x, st.Min), s.fmtTime(st.MinTime, sp.fmt)
			}
			row.Cells = append(row.Cells, c)
		}
		out = append(out, row)
	}
	return out, nil
}

func (s *Server) sensors(ctx context.Context, pc *pageCtx) (Sensors, error) {
	var out Sensors
	now := time.Now().Unix()
	for _, x := range sensorConnections() {
		if _, known := wx.Lookup(x); !known {
			continue
		}
		last, ok, err := s.store.LastSeen(ctx, x, pc.recentDay)
		if err != nil {
			return out, err
		}
		if ok {
			out.Connectivity = append(out.Connectivity, SensorRow{Label: wx.Label(x), Value: wx.FormatValue(x, pc.latest.Get(x), true), Ago: ago(now - last)})
		}
	}
	for _, x := range sensorBatteries() {
		if _, known := wx.Lookup(x); !known {
			continue
		}
		last, ok, err := s.store.LastSeen(ctx, x, pc.recentDay)
		if err != nil {
			return out, err
		}
		if ok {
			status := statusOK
			if v := pc.latest.Get(x); v == nil {
				status = statusUnknown
			} else if *v == 1 {
				status = statusLow
			}
			out.Batteries = append(out.Batteries, SensorRow{Label: wx.Label(x), Status: status, Ago: ago(now - last)})
		}
	}
	return out, nil
}

func orZero(v *float64) *float64 {
	if v == nil {
		z := 0.0
		return &z
	}
	return v
}

func (s *Server) windRows(ctx context.Context, spans []span) ([]HiloRow, error) {
	maxRow := HiloRow{Label: "Max Wind", Kind: kindTwo, Units: unitMph, Units2: unitDeg}
	avgRow := HiloRow{Label: "Average Wind", Kind: kindOne, Units: unitMph}
	rmsRow := HiloRow{Label: "RMS Wind", Kind: kindOne, Units: unitMph}
	vecRow := HiloRow{Label: "Vector Average<br/>Vector Direction", Kind: kindTwo, Units: unitMph, Units2: unitDeg}
	for _, sp := range spans {
		st, err := s.store.Stats(ctx, "wind", sp.start, sp.end)
		if err != nil {
			return nil, err
		}
		maxRow.Cells = append(maxRow.Cells, HiloCell{Span: sp.name, Max: fmtNum(obsWindSpeed, st.Max), MaxTitle: s.fmtTime(st.MaxTime, sp.fmt), Second: fmtNum(obsWindDir, st.MaxDir)})
		avgRow.Cells = append(avgRow.Cells, HiloCell{Span: sp.name, Max: fmtNum(obsWindSpeed, st.Avg)})
		rmsRow.Cells = append(rmsRow.Cells, HiloCell{Span: sp.name, Max: fmtNum(obsWindSpeed, st.RMS)})
		vecRow.Cells = append(vecRow.Cells, HiloCell{Span: sp.name, Max: fmtNum(obsWindSpeed, st.VecAvg), Second: fmtNum(obsWindDir, st.VecDir)})
	}
	return []HiloRow{maxRow, avgRow, rmsRow, vecRow}, nil
}

func (s *Server) observer(pc *pageCtx) almanac.Observer {
	o := almanac.Observer{Lat: s.cfg.Station.Latitude, Lon: s.cfg.Station.Longitude}
	if t, ok := pc.latest.Values[wx.OutTemp]; ok {
		c := wx.FToC(t)
		o.TempC = &c
	}
	if b, ok := pc.latest.Values[obsBarometer]; ok {
		mbar := b * 33.8638866667
		o.PressureMbar = &mbar
	}
	return o
}

func (s *Server) celestial(pc *pageCtx) Celestial {
	o := s.observer(pc)
	sun := o.Sun(pc.ref)
	moon := o.Moon(pc.ref)
	c := Celestial{MoonPhase: moon.PhaseName, MoonFullness: moon.Fullness}
	loc := s.cfg.Station.Timezone
	if sun.HasRise && sun.HasSet {
		c.SunRise = sun.Rise.In(loc).Format(fmtDay)
		c.SunSet = sun.Set.In(loc).Format(fmtDay)
		c.Daylight = fmt.Sprintf("%02d:%02d", int(sun.Daylight.Hours()), int(sun.Daylight.Minutes())%60)
	} else {
		none := "<i>Always down</i>"
		if sun.Altitude >= 0 {
			none = "<i>Always up</i>"
		}
		c.SunRise, c.SunSet = none, none
		c.Daylight = "00:00"
		if sun.Altitude >= 0 {
			c.Daylight = "24:00"
		}
	}
	if moon.HasRise {
		c.MoonRise = moon.Rise.In(loc).Format(fmtDay)
	}
	if moon.HasSet {
		c.MoonSet = moon.Set.In(loc).Format(fmtDay)
	}
	return c
}

func deg(v float64) string { return fmt.Sprintf("%.1f°", v) }

func longForm(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	sec := int(d.Seconds()) % 60
	var parts []string
	plural := func(n int, unit string) string {
		if n == 1 {
			return fmt.Sprintf("%d %s", n, unit)
		}
		return fmt.Sprintf("%d %ss", n, unit)
	}
	if h > 0 {
		parts = append(parts, plural(h, "hour"))
	}
	if m > 0 || h > 0 {
		parts = append(parts, plural(m, "minute"))
	}
	parts = append(parts, plural(sec, "second"))
	return strings.Join(parts, ", ")
}

func (s *Server) buildCelestial(ctx context.Context) (*CelestialPage, error) {
	pc, err := s.context(ctx)
	if err != nil {
		return nil, err
	}
	h, err := s.header(ctx, pc)
	if err != nil {
		return nil, err
	}
	loc := s.cfg.Station.Timezone
	o := s.observer(pc)
	sun := o.Sun(pc.ref)
	moon := o.Moon(pc.ref)
	ev := almanac.NextEvents(pc.ref)
	t := func(v time.Time, ok bool) string {
		if !ok || v.IsZero() {
			return ""
		}
		return v.In(loc).Format(fmtDay)
	}
	sunNone := "<i>Always down</i>"
	if sun.Altitude >= 0 {
		sunNone = "<i>Always up</i>"
	}
	d := CelestialDetail{
		CivilDawn:  t(sun.CivilDawn, true),
		SunRise:    t(sun.Rise, sun.HasRise),
		SunTransit: t(sun.Transit, true),
		SunSet:     t(sun.Set, sun.HasSet),
		CivilDusk:  t(sun.CivilDusk, true),
		SunAz:      deg(sun.Azimuth), SunAlt: deg(sun.Altitude), SunRA: deg(sun.RightAscension), SunDec: deg(sun.Decl),
		Daylight:    longForm(sun.Daylight),
		MoonRise:    t(moon.Rise, moon.HasRise),
		MoonTransit: t(moon.Transit, true),
		MoonSet:     t(moon.Set, moon.HasSet),
		MoonAz:      deg(moon.Azimuth), MoonAlt: deg(moon.Altitude), MoonRA: deg(moon.RightAscension), MoonDec: deg(moon.Decl),
		MoonPhase:    moon.PhaseName,
		MoonFullness: moon.Fullness,
	}
	if !sun.HasRise {
		d.SunRise = sunNone
	}
	if !sun.HasSet {
		d.SunSet = sunNone
	}
	change := "more than yesterday"
	if sun.DaylightChange < 0 {
		change = "less than yesterday"
	}
	d.DaylightChange = longForm(sun.DaylightChange) + " " + change
	eq := Row{"Equinox", ev.NextEquinox.In(loc).Format(fmtLong)}
	so := Row{"Solstice", ev.NextSolstice.In(loc).Format(fmtLong)}
	d.FirstSeason, d.SecondSeason = so, eq
	if ev.NextEquinox.Before(ev.NextSolstice) {
		d.FirstSeason, d.SecondSeason = eq, so
	}
	full := Row{"Full moon", ev.NextFullMoon.In(loc).Format(fmtLong)}
	newm := Row{"New moon", ev.NextNewMoon.In(loc).Format(fmtLong)}
	d.FirstPhase, d.SecondPhase = newm, full
	if ev.NextFullMoon.Before(ev.NextNewMoon) {
		d.FirstPhase, d.SecondPhase = full, newm
	}
	return &CelestialPage{Header: h, Detail: d}, nil
}

func (s *Server) buildStatistics(ctx context.Context) (*StatisticsPage, error) {
	pc, err := s.context(ctx)
	if err != nil {
		return nil, err
	}
	h, err := s.header(ctx, pc)
	if err != nil {
		return nil, err
	}
	rows, err := s.hiloRows(ctx, pc)
	if err != nil {
		return nil, err
	}
	return &StatisticsPage{Header: h, Rows: rows}, nil
}

func (s *Server) buildTelemetry(ctx context.Context) (*TelemetryPage, error) {
	pc, err := s.context(ctx)
	if err != nil {
		return nil, err
	}
	h, err := s.header(ctx, pc)
	if err != nil {
		return nil, err
	}
	sens, err := s.sensors(ctx, pc)
	if err != nil {
		return nil, err
	}
	periods, err := s.periodPlots(ctx, telemetryGroups(), pc.yearStart)
	if err != nil {
		return nil, err
	}
	return &TelemetryPage{Header: h, Sensors: sens, Periods: periods}, nil
}

func dms(v float64, pos, neg string, width int) string {
	h := pos
	if v < 0 {
		h, v = neg, -v
	}
	d := math.Floor(v)
	return fmt.Sprintf("%0*.0f° %05.2f' %s", width, d, (v-d)*60, h)
}

func uptime(d time.Duration) string {
	days := int(d.Hours()) / 24
	return fmt.Sprintf("%d days, %d hours, %d minutes", days, int(d.Hours())%24, int(d.Minutes())%60)
}

func ago(delta int64) string {
	switch {
	case delta < 60:
		return ""
	case delta < 3600:
		return fmt.Sprintf("%d minutes ago", delta/60)
	case delta < 86400:
		return fmt.Sprintf("%d hours ago", delta/3600)
	default:
		return fmt.Sprintf("%d days ago", delta/86400)
	}
}

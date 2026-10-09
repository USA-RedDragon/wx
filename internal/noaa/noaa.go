package noaa

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/USA-RedDragon/wx/internal/store"
	"github.com/USA-RedDragon/wx/internal/wx"
)

const (
	heatBase  = 65.0
	none      = "   N/A"
	noDay     = " N/A"
	hot       = 90.0
	cold      = 32.0
	veryCold  = 0.0
	trace     = 0.01
	someRain  = 0.1
	soak      = 1.0
	ruleDay   = "---------------------------------------------------------------------------------------"
	ruleTemp  = "------------------------------------------------------------------------------------------------"
	ruleRain  = "------------------------------------------------"
	ruleWind  = "-----------------------------------"
	obsTemp   = wx.OutTemp
	obsRain   = wx.Rain
	obsWind   = "wind"
	fmtTemp   = "%6.1f"
	fmtRain   = "%6.2f"
	fmtDir    = "%6.0f"
	fmtCount  = "%6d"
	fmtYM     = "2006 01"
	fmtMonDay = " %02d"
	fmtYrDay  = "  %02d"
)

type Station struct {
	Location     string
	Latitude     float64
	Longitude    float64
	AltitudeFeet float64
	Loc          *time.Location
}

type dayData struct {
	temp, rain, wind *store.DaySum
}

func val(format string, v *float64) string {
	if v == nil || math.IsNaN(*v) {
		return none
	}
	return fmt.Sprintf(format, *v)
}

func ptr(v float64) *float64 { return &v }

func coord(v float64, pos, neg string, width int) string {
	h := pos
	if v < 0 {
		h, v = neg, -v
	}
	deg := math.Floor(v)
	return fmt.Sprintf("%0*.0f-%05.2f %s", width, deg, (v-deg)*60, h)
}

func header(b *strings.Builder, st Station) {
	fmt.Fprintf(b, "NAME: %s                  \n", st.Location)
	fmt.Fprintf(b, "ELEV: %.0f feet    LAT: %s    LONG: %s\n\n\n", st.AltitudeFeet, coord(st.Latitude, "N", "S", 2), coord(st.Longitude, "E", "W", 3))
}

func load(ctx context.Context, s *store.Store, start, end int64) (map[int64]*dayData, error) {
	out := map[int64]*dayData{}
	for _, obs := range []string{obsTemp, obsRain, obsWind} {
		days, err := s.Days(ctx, obs, start, end)
		if err != nil {
			return nil, err
		}
		for i := range days {
			d := days[i]
			dd, ok := out[d.Day]
			if !ok {
				dd = &dayData{}
				out[d.Day] = dd
			}
			switch obs {
			case obsTemp:
				dd.temp = &d
			case obsRain:
				dd.rain = &d
			default:
				dd.wind = &d
			}
		}
	}
	return out, nil
}

func degreeDays(t *store.DaySum) (*float64, *float64) {
	if t == nil || t.Avg() == nil {
		return nil, nil
	}
	a := *t.Avg()
	return ptr(math.Max(0, heatBase-a)), ptr(math.Max(0, a-heatBase))
}

type agg struct {
	wsum, sumtime         float64
	max, min              *float64
	maxT, minT            int64
	heat, cool            float64
	hasHeat               bool
	sumMax, sumMin        float64
	nMax, nMin            int
	geHot, leCold         int
	minLeCold, minLeVCold int
	rain                  float64
	hasRain               bool
	maxRain               *float64
	maxRainT              int64
	rainGe                [3]int
	windW, windT          float64
	windMax               *float64
	windMaxT              int64
	xsum, ysum            float64
	hasTemp, hasWind      bool
}

func (a *agg) add(d *dayData) {
	if t := d.temp; t != nil {
		a.hasTemp = true
		a.wsum += t.WSum
		a.sumtime += float64(t.SumTime)
		if t.Max != nil {
			if a.max == nil || *t.Max > *a.max {
				a.max, a.maxT = t.Max, t.MaxTime
			}
			a.sumMax += *t.Max
			a.nMax++
			if *t.Max >= hot {
				a.geHot++
			}
			if *t.Max <= cold {
				a.leCold++
			}
		}
		if t.Min != nil {
			if a.min == nil || *t.Min < *a.min {
				a.min, a.minT = t.Min, t.MinTime
			}
			a.sumMin += *t.Min
			a.nMin++
			if *t.Min <= cold {
				a.minLeCold++
			}
			if *t.Min <= veryCold {
				a.minLeVCold++
			}
		}
		if h, c := degreeDays(t); h != nil {
			a.heat += *h
			a.cool += *c
			a.hasHeat = true
		}
	}
	if r := d.rain; r != nil {
		a.hasRain = true
		a.rain += r.Sum
		if a.maxRain == nil || r.Sum > *a.maxRain {
			a.maxRain, a.maxRainT = ptr(r.Sum), r.Day+1
		}
		for i, th := range []float64{trace, someRain, soak} {
			if r.Sum >= th {
				a.rainGe[i]++
			}
		}
	}
	if w := d.wind; w != nil {
		a.hasWind = true
		a.windW += w.WSum
		a.windT += float64(w.SumTime)
		if w.Max != nil && (a.windMax == nil || *w.Max > *a.windMax) {
			a.windMax, a.windMaxT = w.Max, w.MaxTime
		}
		a.xsum += w.XSum
		a.ysum += w.YSum
	}
}

func (a *agg) avg() *float64 {
	if a.sumtime == 0 {
		return nil
	}
	return ptr(a.wsum / a.sumtime)
}

func (a *agg) windAvg() *float64 {
	if a.windT == 0 {
		return nil
	}
	return ptr(a.windW / a.windT)
}

func (a *agg) vecDir() *float64 {
	if a.xsum == 0 && a.ysum == 0 {
		return nil
	}
	return ptr(math.Mod(math.Atan2(a.xsum, a.ysum)*180/math.Pi+360, 360))
}

func (a *agg) heatCool() (*float64, *float64) {
	if !a.hasHeat {
		return nil, nil
	}
	return ptr(a.heat), ptr(a.cool)
}

func mean(sum float64, n int) *float64 {
	if n == 0 {
		return nil
	}
	return ptr(sum / float64(n))
}

func tm(ts int64, loc *time.Location, layout string, has bool) string {
	if !has {
		return noDay
	}
	return time.Unix(ts, 0).In(loc).Format(layout)
}

func day(ts int64, loc *time.Location, layout string, has bool) string {
	if !has {
		return noDay
	}
	return fmt.Sprintf(layout, time.Unix(ts, 0).In(loc).Day())
}

func Month(ctx context.Context, s *store.Store, st Station, year int, month time.Month) (string, error) {
	start := time.Date(year, month, 1, 0, 0, 0, 0, st.Loc)
	end := start.AddDate(0, 1, 0)
	data, err := load(ctx, s, start.Unix(), end.Unix())
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "                   MONTHLY CLIMATOLOGICAL SUMMARY for %s %d\n\n\n", start.Format("Jan"), year)
	header(&b, st)
	b.WriteString("                   TEMPERATURE (F), RAIN (in), WIND SPEED (mph)\n\n")
	b.WriteString("                                         HEAT   COOL         AVG\n")                        //nolint:dupword
	b.WriteString("      MEAN                               DEG    DEG          WIND                   DOM\n") //nolint:dupword
	b.WriteString("DAY   TEMP   HIGH   TIME    LOW   TIME   DAYS   DAYS   RAIN  SPEED   HIGH   TIME    DIR\n") //nolint:dupword
	b.WriteString(ruleDay + "\n")
	var total agg
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		dd, ok := data[d.Unix()]
		if !ok {
			fmt.Fprintf(&b, fmtMonDay+"\n", d.Day())
			continue
		}
		total.add(dd)
		var a agg
		a.add(dd)
		heat, cool := a.heatCool()
		var rain *float64
		if a.hasRain {
			rain = ptr(a.rain)
		}
		fmt.Fprintf(&b, fmtMonDay+" %s %s %s %s %s %s %s %s %s %s %s %s\n", d.Day(),
			val(fmtTemp, a.avg()), val(fmtTemp, a.max), tm(a.maxT, st.Loc, " 15:04", a.max != nil),
			val(fmtTemp, a.min), tm(a.minT, st.Loc, " 15:04", a.min != nil),
			val(fmtTemp, heat), val(fmtTemp, cool), val(fmtRain, rain),
			val(fmtTemp, a.windAvg()), val(fmtTemp, a.windMax), tm(a.windMaxT, st.Loc, " 15:04", a.windMax != nil),
			val(fmtDir, a.vecDir()))
	}
	if total.hasTemp || total.hasRain || total.hasWind {
		b.WriteString(ruleDay + "\n")
		heat, cool := total.heatCool()
		var rain *float64
		if total.hasRain {
			rain = ptr(total.rain)
		}
		fmt.Fprintf(&b, "    %s %s    %s %s    %s %s %s %s %s %s    %s %s\n",
			val(fmtTemp, total.avg()), val(fmtTemp, total.max), day(total.maxT, st.Loc, fmtMonDay, total.max != nil),
			val(fmtTemp, total.min), day(total.minT, st.Loc, fmtMonDay, total.min != nil),
			val(fmtTemp, heat), val(fmtTemp, cool), val(fmtRain, rain),
			val(fmtTemp, total.windAvg()), val(fmtTemp, total.windMax), day(total.windMaxT, st.Loc, fmtMonDay, total.windMax != nil),
			val(fmtDir, total.vecDir()))
	}
	return b.String(), nil
}

func count(n int, has bool) string {
	if !has {
		return none
	}
	return fmt.Sprintf(fmtCount, n)
}

func Year(ctx context.Context, s *store.Store, st Station, year int) (string, error) {
	start := time.Date(year, 1, 1, 0, 0, 0, 0, st.Loc)
	end := start.AddDate(1, 0, 0)
	data, err := load(ctx, s, start.Unix(), end.Unix())
	if err != nil {
		return "", err
	}
	months := make([]agg, 12)
	var total agg
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		if dd, ok := data[d.Unix()]; ok {
			months[d.Month()-1].add(dd)
			total.add(dd)
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "                     CLIMATOLOGICAL SUMMARY for year %d\n\n\n", year)
	header(&b, st)
	b.WriteString("                                       TEMPERATURE (F)\n\n")
	b.WriteString("                              HEAT    COOL                              MAX    MAX    MIN    MIN\n")                                      //nolint:dupword
	b.WriteString("          MEAN   MEAN         DEG     DEG                                >=     <=     <=     <=\n")                                      //nolint:dupword
	fmt.Fprintf(&b, " YR  MO   MAX    MIN    MEAN  DAYS    DAYS      HI  DAY     LOW  DAY %6d %6d %6d %6d\n", int(hot), int(cold), int(cold), int(veryCold)) //nolint:dupword
	b.WriteString(ruleTemp + "\n")
	tempLine := func(prefix string, a *agg, dayFmt func(int64, bool) string) string {
		heat, cool := a.heatCool()
		return fmt.Sprintf("%s %s %s %s %s %s  %s %s  %s %s %s %s %s %s", prefix,
			val(fmtTemp, mean(a.sumMax, a.nMax)), val(fmtTemp, mean(a.sumMin, a.nMin)), val(fmtTemp, a.avg()),
			val(fmtTemp, heat), val(fmtTemp, cool),
			val(fmtTemp, a.max), dayFmt(a.maxT, a.max != nil), val(fmtTemp, a.min), dayFmt(a.minT, a.min != nil),
			count(a.geHot, a.nMax > 0), count(a.leCold, a.nMax > 0), count(a.minLeCold, a.nMin > 0), count(a.minLeVCold, a.nMin > 0))
	}
	monthDay := func(ts int64, has bool) string { return day(ts, st.Loc, fmtYrDay, has) }
	monthName := func(ts int64, has bool) string { return tm(ts, st.Loc, " Jan", has) }
	for i := range months {
		ym := time.Date(year, time.Month(i+1), 1, 0, 0, 0, 0, st.Loc).Format(fmtYM)
		if !months[i].hasTemp {
			b.WriteString(ym + "\n")
			continue
		}
		b.WriteString(tempLine(ym, &months[i], monthDay) + "\n")
	}
	if total.hasTemp {
		b.WriteString(ruleTemp + "\n")
		b.WriteString(tempLine("       ", &total, monthName) + "\n")
	}
	b.WriteString("\n\n                  PRECIPITATION (in)\n\n")
	b.WriteString("                  MAX         ---DAYS OF RAIN---\n")
	b.WriteString("                  OBS.               OVER\n")
	fmt.Fprintf(&b, " YR  MO  TOTAL    DAY  DATE %6.2f %6.2f %6.2f\n", trace, someRain, soak)
	b.WriteString(ruleRain + "\n")
	rainLine := func(prefix string, a *agg, dayFmt func(int64, bool) string) string {
		return fmt.Sprintf("%s %s %s  %s %s %s %s", prefix, val(fmtRain, ptr(a.rain)), val(fmtRain, a.maxRain), dayFmt(a.maxRainT, a.maxRain != nil),
			count(a.rainGe[0], true), count(a.rainGe[1], true), count(a.rainGe[2], true))
	}
	for i := range months {
		ym := time.Date(year, time.Month(i+1), 1, 0, 0, 0, 0, st.Loc).Format(fmtYM)
		if !months[i].hasRain {
			b.WriteString(ym + "\n")
			continue
		}
		b.WriteString(rainLine(ym, &months[i], monthDay) + "\n")
	}
	if total.hasRain {
		b.WriteString(ruleRain + "\n")
		b.WriteString(rainLine("       ", &total, monthName) + "\n")
	}
	b.WriteString("\n\n           WIND SPEED (mph)\n\n")
	b.WriteString("                                DOM\n")
	b.WriteString(" YR  MO    AVG     HI   DATE    DIR\n")
	b.WriteString(ruleWind + "\n")
	windLine := func(prefix string, a *agg, dayFmt func(int64, bool) string) string {
		return fmt.Sprintf("%s %s %s   %s %s", prefix, val(fmtTemp, a.windAvg()), val(fmtTemp, a.windMax), dayFmt(a.windMaxT, a.windMax != nil), val(fmtDir, a.vecDir()))
	}
	for i := range months {
		ym := time.Date(year, time.Month(i+1), 1, 0, 0, 0, 0, st.Loc).Format(fmtYM)
		if !months[i].hasWind {
			b.WriteString(ym + "\n")
			continue
		}
		b.WriteString(windLine(ym, &months[i], monthDay) + "\n")
	}
	if total.hasWind {
		b.WriteString(ruleWind + "\n")
		b.WriteString(windLine("       ", &total, monthName) + "\n")
	}
	return b.String(), nil
}

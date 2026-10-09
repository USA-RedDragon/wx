package almanac

import (
	"math"
	"time"

	"github.com/soniakeys/meeus/v3/base"
	"github.com/soniakeys/meeus/v3/coord"
	"github.com/soniakeys/meeus/v3/deltat"
	"github.com/soniakeys/meeus/v3/globe"
	"github.com/soniakeys/meeus/v3/julian"
	"github.com/soniakeys/meeus/v3/moonillum"
	"github.com/soniakeys/meeus/v3/moonphase"
	"github.com/soniakeys/meeus/v3/moonposition"
	"github.com/soniakeys/meeus/v3/nutation"
	"github.com/soniakeys/meeus/v3/parallax"
	"github.com/soniakeys/meeus/v3/sidereal"
	"github.com/soniakeys/meeus/v3/solar"
	"github.com/soniakeys/meeus/v3/solstice"
	"github.com/soniakeys/unit"
)

const (
	auKm           = 149597870.7
	earthRadiusKm  = 6378.14
	searchStep     = 10 * time.Minute
	bisectionLimit = 500 * time.Millisecond
	synodicMonth   = 29.530588
	newMoon2018    = 1516155420
	stdPressure    = 1010.0
	stdTempC       = 15.0
	civilTwilight  = -6.0
	horizonRefr    = 37.3 / 60
	twilightRefr   = 0.155
)

type Observer struct {
	Lat          float64
	Lon          float64
	TempC        *float64
	PressureMbar *float64
}

type Body struct {
	Rise, Transit, Set   time.Time
	HasRise, HasSet      bool
	Azimuth, Altitude    float64
	RightAscension, Decl float64
}

type Sun struct {
	Body
	CivilDawn, CivilDusk time.Time
	Daylight             time.Duration
	DaylightChange       time.Duration
}

type Moon struct {
	Body
	Fullness  int
	PhaseName string
}

type Events struct {
	NextEquinox, NextSolstice time.Time
	NextNewMoon, NextFullMoon time.Time
}

func phaseNames() []string {
	return []string{"New", "Waxing crescent", "First quarter", "Waxing gibbous", "Full", "Waning gibbous", "Last quarter", "Waning crescent"}
}

func jde(t time.Time) (float64, float64) {
	jd := julian.TimeToJD(t.UTC())
	year := float64(t.Year()) + float64(t.YearDay())/365.25
	return jd, jd + deltat.PolyAfter2000(year).Sec()/86400
}

func timeFromJDE(j float64) time.Time {
	y, _, _ := julian.JDToCalendar(j)
	return julian.JDToTime(j - deltat.PolyAfter2000(float64(y)).Sec()/86400)
}

func sunEquatorial(t time.Time) (unit.RA, unit.Angle, float64) {
	_, je := jde(t)
	ra, dec := solar.ApparentEquatorial(je)
	return ra, dec, solar.Radius(base.J2000Century(je))
}

func moonEquatorial(t time.Time) (unit.RA, unit.Angle, float64) {
	_, je := jde(t)
	lon, lat, dist := moonposition.Position(je)
	dpsi, deps := nutation.Nutation(je)
	eps := nutation.MeanObliquity(je) + deps
	ra, dec := coord.EclToEq(lon+dpsi, lat, eps.Sin(), eps.Cos())
	return ra, dec, dist
}

func (o Observer) horizontal(t time.Time, ra unit.RA, dec unit.Angle) (float64, float64) {
	jd, _ := jde(t)
	st := sidereal.Apparent(jd)
	a, h := coord.EqToHz(ra, dec, unit.AngleFromDeg(o.Lat), unit.AngleFromDeg(-o.Lon), st)
	return math.Mod(a.Deg()+180+360, 360), h.Deg()
}

func (o Observer) hourAngle(t time.Time, ra unit.RA) float64 {
	jd, _ := jde(t)
	st := sidereal.Apparent(jd)
	h := st.Rad() - unit.AngleFromDeg(-o.Lon).Rad() - ra.Rad()
	return math.Remainder(h, 2*math.Pi)
}

func (o Observer) refraction() float64 {
	p, tc := stdPressure, stdTempC
	if o.PressureMbar != nil {
		p = *o.PressureMbar
	}
	if o.TempC != nil {
		tc = *o.TempC
	}
	return (p / stdPressure) * (283 / (273 + tc))
}

func (o Observer) sunAlt(t time.Time) float64 {
	ra, dec, _ := sunEquatorial(t)
	_, alt := o.horizontal(t, ra, dec)
	return alt
}

func (o Observer) moonAlt(t time.Time) (float64, float64) {
	ra, dec, dist := moonEquatorial(t)
	_, alt := o.horizontal(t, ra, dec)
	parallax := math.Asin(earthRadiusKm/dist) * 180 / math.Pi
	return alt, parallax
}

type crossing struct {
	t      time.Time
	rising bool
}

func search(start, end time.Time, f func(time.Time) float64) []crossing {
	var out []crossing
	prevT, prev := start, f(start)
	for t := start.Add(searchStep); !t.After(end); t = t.Add(searchStep) {
		cur := f(t)
		if (prev < 0) != (cur < 0) {
			lo, hi, flo := prevT, t, prev
			for hi.Sub(lo) > bisectionLimit {
				mid := lo.Add(hi.Sub(lo) / 2)
				fm := f(mid)
				if (fm < 0) == (flo < 0) {
					lo, flo = mid, fm
				} else {
					hi = mid
				}
			}
			out = append(out, crossing{t: lo.Add(hi.Sub(lo) / 2).Round(time.Second), rising: prev < 0})
		}
		prevT, prev = t, cur
	}
	return out
}

func dayBounds(day time.Time) (time.Time, time.Time) {
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	return start, start.AddDate(0, 0, 1)
}

func pick(cs []crossing) (time.Time, time.Time, bool, bool) {
	var rise, set time.Time
	var hasRise, hasSet bool
	for _, c := range cs {
		if c.rising && !hasRise {
			rise, hasRise = c.t, true
		}
		if !c.rising && !hasSet {
			set, hasSet = c.t, true
		}
	}
	return rise, set, hasRise, hasSet
}

func transit(start, end time.Time, ha func(time.Time) float64) time.Time {
	for _, c := range search(start, end, ha) {
		if c.rising {
			return c.t
		}
	}
	return time.Time{}
}

func (o Observer) sunDay(day time.Time) (time.Time, time.Time, bool, bool) {
	start, end := dayBounds(day)
	h0 := -horizonRefr * o.refraction()
	return pick(search(start, end, func(t time.Time) float64 {
		_, _, r := sunEquatorial(t)
		return o.sunAlt(t) - (h0 - 0.2666/r)
	}))
}

func (o Observer) Sun(at time.Time) Sun {
	start, end := dayBounds(at)
	var s Sun
	s.Rise, s.Set, s.HasRise, s.HasSet = o.sunDay(at)
	s.Transit = transit(start, end, func(t time.Time) float64 {
		ra, _, _ := sunEquatorial(t)
		return o.hourAngle(t, ra)
	})
	s.CivilDawn, s.CivilDusk, _, _ = pick(search(start, end, func(t time.Time) float64 { return o.sunAlt(t) - (civilTwilight - twilightRefr*o.refraction()) }))
	ra, dec, _ := sunEquatorial(at)
	s.Azimuth, s.Altitude = o.horizontal(at, ra, dec)
	s.RightAscension, s.Decl = ra.Deg(), dec.Deg()
	if s.HasRise && s.HasSet {
		s.Daylight = s.Set.Sub(s.Rise)
		pr, ps, okR, okS := o.sunDay(at.AddDate(0, 0, -1))
		if okR && okS {
			s.DaylightChange = s.Daylight - ps.Sub(pr)
		}
	}
	return s
}

func (o Observer) Moon(at time.Time) Moon {
	start, end := dayBounds(at)
	var m Moon
	r := horizonRefr * o.refraction()
	m.Rise, m.Set, m.HasRise, m.HasSet = pick(search(start, end, func(t time.Time) float64 {
		alt, par := o.moonAlt(t)
		return alt - (0.7275*par - r)
	}))
	m.Transit = transit(start, end, func(t time.Time) float64 {
		ra, _, _ := moonEquatorial(t)
		return o.hourAngle(t, ra)
	})
	ra, dec, dist := moonEquatorial(at)
	_, je := jde(at)
	rs, rc := globe.Earth76.ParallaxConstants(unit.AngleFromDeg(o.Lat), 0)
	tra, tdec := parallax.Topocentric(ra, dec, dist/auKm, rs, rc, unit.AngleFromDeg(-o.Lon), je)
	m.Azimuth, m.Altitude = o.horizontal(at, tra, tdec)
	m.RightAscension, m.Decl = tra.Deg(), tdec.Deg()
	sra, sdec, sr := sunEquatorial(at)
	i := moonillum.PhaseAngleEq(ra, dec, dist, sra, sdec, sr*auKm)
	m.Fullness = int((1+i.Cos())/2*100 + 0.5)
	m.PhaseName = PhaseName(at)
	return m
}

func PhaseName(t time.Time) string {
	pos := math.Mod(float64(t.Unix()-newMoon2018)/86400/synodicMonth, 1)
	if pos < 0 {
		pos++
	}
	return phaseNames()[int(pos*8+0.5)&7]
}

func decimalYear(t time.Time) float64 {
	return float64(t.Year()) + float64(t.YearDay()-1)/365.25
}

func next(after time.Time, f func(float64) float64) time.Time {
	y := decimalYear(after) - synodicMonth/365.25
	for range 4 {
		t := timeFromJDE(f(y))
		if t.After(after) {
			return t
		}
		y += synodicMonth / 365.25
	}
	return time.Time{}
}

func NextEvents(after time.Time) Events {
	var e Events
	e.NextNewMoon = next(after, moonphase.New)
	e.NextFullMoon = next(after, moonphase.Full)
	for y := after.Year(); y <= after.Year()+1; y++ {
		for _, eq := range []float64{solstice.March(y), solstice.September(y)} {
			if t := timeFromJDE(eq); t.After(after) && e.NextEquinox.IsZero() {
				e.NextEquinox = t
			}
		}
		for _, so := range []float64{solstice.June(y), solstice.December(y)} {
			if t := timeFromJDE(so); t.After(after) && e.NextSolstice.IsZero() {
				e.NextSolstice = t
			}
		}
	}
	return e
}

func (o Observer) SunRiseSet(day time.Time) (time.Time, time.Time, bool) {
	rise, set, okR, okS := o.sunDay(day)
	return rise, set, okR && okS
}

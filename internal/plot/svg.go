package plot

import (
	"bytes"
	"fmt"
	"html"
	"math"
	"time"

	"github.com/USA-RedDragon/wx/internal/almanac"
	"github.com/USA-RedDragon/wx/internal/wx"
)

const (
	width      = 1000
	height     = 360
	padLeft    = 70.0
	padRight   = 30.0
	padTop     = 38.0
	padBottom  = 62.0
	bgColor    = "#101010"
	gridColor  = "#1c1c1c"
	textColor  = "#eeeeee"
	dayColor   = "#333333"
	nightColor = "#101010"
	edgeColor  = "#ff4500"
	gapFrac    = 0.01
)

func lineColors() []string {
	return []string{"#4282b4", "#b44242", "#42b442", "#42b4b4", "#b442b4"}
}

type Point struct {
	T int64
	V float64
	X float64
	Y float64
}

type Data struct {
	Series [][]Point
}

type Frame struct {
	Start, End   int64
	Loc          *time.Location
	Lat, Lng     float64
	BottomLabel  string
	Period       Period
	Spec         Spec
	UnitOverride string
}

func niceStep(span float64, target int) float64 {
	if span <= 0 || math.IsNaN(span) {
		return 1
	}
	raw := span / float64(target)
	mag := math.Pow(10, math.Floor(math.Log10(raw)))
	n := raw / mag
	switch {
	case n < 1.5:
		n = 1
	case n < 3:
		n = 2
	case n < 7:
		n = 5
	default:
		n = 10
	}
	return n * mag
}

func scale(data Data, ys YScale, kind Kind) (lo, hi, step float64) {
	lo, hi = math.Inf(1), math.Inf(-1)
	for _, s := range data.Series {
		for _, p := range s {
			v := p.V
			if kind == Vector {
				lo = math.Min(lo, -p.V)
				hi = math.Max(hi, p.V)
				continue
			}
			lo, hi = math.Min(lo, v), math.Max(hi, v)
		}
	}
	if math.IsInf(lo, 1) {
		lo, hi = 0, 1
	}
	if kind == Bar {
		lo = math.Min(lo, 0)
	}
	if ys.Min != nil {
		lo = *ys.Min
	}
	if ys.Max != nil {
		hi = *ys.Max
	}
	if hi <= lo {
		hi = lo + 1
	}
	if ys.Step != nil && ys.Min != nil && ys.Max != nil {
		return lo, hi, *ys.Step
	}
	step = niceStep(hi-lo, 5)
	if ys.Step != nil && *ys.Step > step {
		step = *ys.Step
	}
	if ys.Min == nil {
		lo = math.Floor(lo/step) * step
	}
	if ys.Max == nil {
		hi = math.Ceil(hi/step) * step
		if hi == lo {
			hi = lo + step
		}
	}
	return lo, hi, step
}

func fmtTick(v, step float64) string {
	dec := 0
	if step < 1 {
		dec = int(math.Ceil(-math.Log10(step)))
	}
	s := fmt.Sprintf("%.*f", dec, v)
	if s == "-0" {
		s = "0"
	}
	return s
}

func xTicks(fr Frame) []int64 {
	start := time.Unix(fr.Start, 0).In(fr.Loc)
	var ticks []int64
	var t time.Time
	var stepFn func(time.Time) time.Time
	switch fr.Period.Name {
	case PeriodDay:
		t = time.Date(start.Year(), start.Month(), start.Day(), start.Hour(), 0, 0, 0, fr.Loc)
		for t.Hour()%6 != 0 || t.Unix() < fr.Start {
			t = t.Add(time.Hour)
		}
		stepFn = func(x time.Time) time.Time { return x.Add(6 * time.Hour) }
	case PeriodWeek:
		t = time.Date(start.Year(), start.Month(), start.Day()+1, 0, 0, 0, 0, fr.Loc)
		stepFn = func(x time.Time) time.Time { return x.AddDate(0, 0, 1) }
	case PeriodMonth:
		t = time.Date(start.Year(), start.Month(), start.Day()+1, 0, 0, 0, 0, fr.Loc)
		for t.Day()%3 != 0 {
			t = t.AddDate(0, 0, 1)
		}
		stepFn = func(x time.Time) time.Time { return x.AddDate(0, 0, 3) }
	default:
		t = time.Date(start.Year(), start.Month()+1, 1, 0, 0, 0, 0, fr.Loc)
		stepFn = func(x time.Time) time.Time { return x.AddDate(0, 1, 0) }
	}
	for ; t.Unix() <= fr.End; t = stepFn(t) {
		ticks = append(ticks, t.Unix())
	}
	return ticks
}

func Render(fr Frame, data Data) []byte {
	var b bytes.Buffer
	pw := width - padLeft - padRight
	ph := height - padTop - padBottom
	kind := fr.Spec.Kind
	lo, hi, step := scale(data, fr.Spec.YScale, kind)
	xOf := func(t int64) float64 {
		return padLeft + float64(t-fr.Start)/float64(fr.End-fr.Start)*pw
	}
	yOf := func(v float64) float64 {
		return padTop + ph - (v-lo)/(hi-lo)*ph
	}
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="%d" height="%d" font-family="Open Sans, sans-serif">`, width, height, width, height)
	fmt.Fprintf(&b, `<rect width="100%%" height="100%%" fill="%s"/>`, bgColor)
	fmt.Fprintf(&b, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s"/>`, padLeft, padTop, pw, ph, bgColor)
	if fr.Period.DayNight {
		renderDayNight(&b, fr, xOf, ph)
	}
	for v := lo; v <= hi+step/1000; v += step {
		y := yOf(v)
		fmt.Fprintf(&b, `<line x1="%.1f" x2="%.1f" y1="%.1f" y2="%.1f" stroke="%s" stroke-width="1"/>`, padLeft, padLeft+pw, y, y, gridColor)
		fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" fill="%s" font-size="16" text-anchor="end" dominant-baseline="middle">%s</text>`, padLeft-6, y, textColor, fmtTick(v, step))
	}
	for _, t := range xTicks(fr) {
		x := xOf(t)
		fmt.Fprintf(&b, `<line x1="%.1f" x2="%.1f" y1="%.1f" y2="%.1f" stroke="%s" stroke-width="1"/>`, x, x, padTop, padTop+ph, gridColor)
		lbl := time.Unix(t, 0).In(fr.Loc).Format(fr.Period.XFormat)
		fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" fill="%s" font-size="16" text-anchor="middle">%s</text>`, x, padTop+ph+20, textColor, html.EscapeString(lbl))
	}
	fmt.Fprintf(&b, `<clipPath id="c"><rect x="%.1f" y="%.1f" width="%.1f" height="%.1f"/></clipPath><g clip-path="url(#c)">`, padLeft, padTop, pw, ph)
	for i, s := range data.Series {
		colors := lineColors()
		color := colors[i%len(colors)]
		switch kind {
		case Bar:
			agg := fr.Spec.Series[i].AggI
			bw := float64(agg) / float64(time.Duration(fr.End-fr.Start)*time.Second) * pw
			for _, p := range s {
				x := xOf(p.T) - bw
				y := yOf(math.Max(p.V, lo))
				fmt.Fprintf(&b, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s" stroke="%s" stroke-width="1"/>`, x, y, math.Max(bw, 1), yOf(math.Max(lo, 0))-y, "#72b2c4", color)
			}
		case Markers:
			for _, p := range s {
				fmt.Fprintf(&b, `<rect x="%.1f" y="%.1f" width="2" height="2" fill="%s"/>`, xOf(p.T)-1, yOf(p.V)-1, color)
			}
		case Vector:
			y0 := yOf(0)
			k := ph / (hi - lo)
			for _, p := range s {
				x1 := xOf(p.T)
				fmt.Fprintf(&b, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="1"/>`, x1, y0, x1+p.Y*k, y0+p.X*k, color)
			}
		case Line:
			renderLine(&b, s, fr, xOf, yOf, color)
		}
	}
	b.WriteString(`</g>`)
	renderLabels(&b, fr, pw)
	b.WriteString(`</svg>`)
	return b.Bytes()
}

func renderLine(b *bytes.Buffer, s []Point, fr Frame, xOf func(int64) float64, yOf func(float64) float64, color string) {
	maxGap := int64(gapFrac * float64(fr.End-fr.Start))
	if fr.Period.AggI > 0 {
		maxGap = max(maxGap, int64(fr.Period.AggI/time.Second)*2)
	}
	var path bytes.Buffer
	var prev int64
	for i, p := range s {
		cmd := "L"
		if i == 0 || p.T-prev > maxGap {
			cmd = "M"
		}
		fmt.Fprintf(&path, "%s%.1f %.1f", cmd, xOf(p.T), yOf(p.V))
		prev = p.T
	}
	if path.Len() > 0 {
		fmt.Fprintf(b, `<path d="%s" fill="none" stroke="%s" stroke-width="1"/>`, path.String(), color)
	}
}

func renderDayNight(b *bytes.Buffer, fr Frame, xOf func(int64) float64, ph float64) {
	fmt.Fprintf(b, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s"/>`, padLeft, padTop, xOf(fr.End)-padLeft, ph, nightColor)
	day := time.Unix(fr.Start, 0).In(fr.Loc).AddDate(0, 0, -1)
	for !day.After(time.Unix(fr.End, 0).In(fr.Loc).AddDate(0, 0, 1)) {
		rise, set, ok := almanac.Observer{Lat: fr.Lat, Lon: fr.Lng}.SunRiseSet(day)
		if ok {
			x1 := math.Max(xOf(rise.Unix()), padLeft)
			x2 := math.Min(xOf(set.Unix()), xOf(fr.End))
			if x2 > x1 {
				fmt.Fprintf(b, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s"/>`, x1, padTop, x2-x1, ph, dayColor)
				for _, x := range []float64{xOf(rise.Unix()), xOf(set.Unix())} {
					if x > padLeft && x < xOf(fr.End) {
						fmt.Fprintf(b, `<line x1="%.1f" x2="%.1f" y1="%.1f" y2="%.1f" stroke="%s" stroke-opacity="0.35" stroke-width="2"/>`, x, x, padTop, padTop+ph, edgeColor)
					}
				}
			}
		}
		day = day.AddDate(0, 0, 1)
	}
}

func seriesLabel(s Series) string {
	if s.Label != "" {
		return s.Label
	}
	return wx.Label(s.Obs)
}

func renderLabels(b *bytes.Buffer, fr Frame, pw float64) {
	fmt.Fprintf(b, `<text x="%.1f" y="24" font-size="16" font-weight="bold" text-anchor="middle">`, padLeft+pw/2)
	for i, s := range fr.Spec.Series {
		if i > 0 {
			b.WriteString(`<tspan> </tspan>`)
		}
		colors := lineColors()
		fmt.Fprintf(b, `<tspan fill="%s">%s</tspan>`, colors[i%len(colors)], html.EscapeString(seriesLabel(s)))
	}
	b.WriteString(`</text>`)
	unit := fr.UnitOverride
	if unit == "" && len(fr.Spec.Series) > 0 {
		unit = wx.UnitLabel(fr.Spec.Series[0].Obs)
	}
	fmt.Fprintf(b, `<text x="6" y="20" fill="%s" font-size="16" font-weight="bold">%s</text>`, textColor, html.EscapeString(unitAbbrev(unit)))
	fmt.Fprintf(b, `<text x="%.1f" y="%d" fill="%s" font-size="16" text-anchor="middle">%s</text>`, padLeft+pw/2, height-12, textColor, html.EscapeString(fr.BottomLabel))
}

func unitAbbrev(u string) string {
	switch u {
	case " in/h":
		return "in"
	case " feet":
		return "feet"
	}
	if len(u) > 0 && u[0] == ' ' {
		return u[1:]
	}
	return u
}

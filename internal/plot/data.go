package plot

import (
	"math"
	"time"

	"github.com/USA-RedDragon/wx/internal/store"
	"github.com/USA-RedDragon/wx/internal/wx"
)

func bucketEnd(ts int64, agg time.Duration, loc *time.Location) int64 {
	secs := int64(agg / time.Second)
	if secs >= 86400 {
		t := time.Unix(ts-1, 0).In(loc)
		days := int(secs / 86400)
		d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
		if days == 7 {
			d = d.AddDate(0, 0, -int(d.Weekday()))
		}
		return d.AddDate(0, 0, days).Unix()
	}
	_, off := time.Unix(ts, 0).In(loc).Zone()
	local := ts + int64(off) - 1
	return (local/secs+1)*secs - int64(off)
}

type acc struct {
	sum, max  float64
	n         int
	xs, ys    float64
	hasMax    bool
	weightSum float64
}

type seriesBuilder struct {
	s       Series
	agg     Agg
	aggI    time.Duration
	raw     []Point
	buckets map[int64]*acc
	order   []int64
}

type Builder struct {
	fr     Frame
	series []*seriesBuilder
}

func NewBuilder(fr Frame) *Builder {
	b := &Builder{fr: fr}
	for _, s := range fr.Spec.Series {
		agg, aggI := s.Agg, s.AggI
		if agg == AggNone && fr.Period.Agg != AggNone {
			agg = fr.Period.Agg
		}
		if aggI == 0 {
			aggI = fr.Period.AggI
		}
		if aggI == 0 {
			agg = AggNone
		}
		b.series = append(b.series, &seriesBuilder{s: s, agg: agg, aggI: aggI, buckets: map[int64]*acc{}})
	}
	return b
}

func Columns(spec Spec) []string {
	cols := Obs(spec)
	if spec.Kind == Vector {
		return []string{wx.WindSpeed, wx.WindDir}
	}
	for _, s := range spec.Series {
		if s.Agg == AggVecDir || s.Obs == wx.WindDir {
			cols = append(cols, wx.WindSpeed)
		}
	}
	return cols
}

func (b *Builder) Add(r store.Record) {
	for _, sb := range b.series {
		if b.fr.Spec.Kind == Vector {
			sb.addVector(r, b.fr.Loc)
			continue
		}
		v, ok := r.Values[sb.s.Obs]
		if sb.s.Obs == wx.WindDir && sb.agg == AggVecDir {
			v, ok = r.Values[wx.WindSpeed]
		}
		if !ok || math.IsNaN(v) {
			continue
		}
		if sb.agg == AggNone {
			sb.raw = append(sb.raw, Point{T: r.DateTime, V: v})
			continue
		}
		end := bucketEnd(r.DateTime, sb.aggI, b.fr.Loc)
		a, ok := sb.buckets[end]
		if !ok {
			a = &acc{}
			sb.buckets[end] = a
			sb.order = append(sb.order, end)
		}
		w := float64(max(r.Interval, 1))
		a.sum += v * w
		a.weightSum += w
		a.n++
		if !a.hasMax || v > a.max {
			a.max, a.hasMax = v, true
		}
		switch sb.agg {
		case AggNone, AggAvg, AggMax:
		case AggSum:
			a.xs += v
		case AggVecDir:
			if d, ok := r.Values[wx.WindDir]; ok {
				rad := d * math.Pi / 180
				a.xs += v * math.Sin(rad) * w
				a.ys += v * math.Cos(rad) * w
			}
		}
	}
}

func (sb *seriesBuilder) addVector(r store.Record, loc *time.Location) {
	s, ok1 := r.Values[wx.WindSpeed]
	d, ok2 := r.Values[wx.WindDir]
	if !ok1 || !ok2 {
		return
	}
	rad := d * math.Pi / 180
	if sb.aggI == 0 {
		sb.raw = append(sb.raw, Point{T: r.DateTime, V: s, X: s * math.Sin(rad), Y: s * math.Cos(rad)})
		return
	}
	end := bucketEnd(r.DateTime, sb.aggI, loc)
	a, ok := sb.buckets[end]
	if !ok {
		a = &acc{}
		sb.buckets[end] = a
		sb.order = append(sb.order, end)
	}
	w := float64(max(r.Interval, 1))
	a.xs += s * math.Sin(rad) * w
	a.ys += s * math.Cos(rad) * w
	a.weightSum += w
}

func (b *Builder) Data() Data {
	out := Data{Series: make([][]Point, len(b.series))}
	for i, sb := range b.series {
		if sb.raw != nil || len(sb.order) == 0 {
			out.Series[i] = sb.raw
			continue
		}
		pts := make([]Point, 0, len(sb.order))
		for _, end := range sb.order {
			a := sb.buckets[end]
			if b.fr.Spec.Kind == Vector {
				x, y := a.xs/a.weightSum, a.ys/a.weightSum
				pts = append(pts, Point{T: end, V: math.Hypot(x, y), X: x, Y: y})
				continue
			}
			var v float64
			switch sb.agg {
			case AggSum:
				v = a.xs
			case AggMax:
				v = a.max
			case AggVecDir:
				if a.xs == 0 && a.ys == 0 {
					continue
				}
				v = math.Mod(math.Atan2(a.xs, a.ys)*180/math.Pi+360, 360)
			case AggNone, AggAvg:
				v = a.sum / a.weightSum
			}
			pts = append(pts, Point{T: end, V: v})
		}
		out.Series[i] = pts
	}
	return out
}

func Build(recs []store.Record, fr Frame) Data {
	b := NewBuilder(fr)
	for _, r := range recs {
		b.Add(r)
	}
	return b.Data()
}

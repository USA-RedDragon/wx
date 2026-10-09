package importer

import (
	"math"

	"github.com/USA-RedDragon/wx/internal/store"
	"github.com/USA-RedDragon/wx/internal/wx"
)

type bucket struct {
	end    int64
	sums   map[string]float64
	counts map[string]int
	hi     map[string]store.Extreme
	lo     map[string]store.Extreme
	last   map[string]float64
	xsum   float64
	ysum   float64
	gust   float64
	gustAt int64
	gustD  *float64
	hasG   bool
}

func newBucket(end int64) *bucket {
	return &bucket{
		end:    end,
		sums:   map[string]float64{},
		counts: map[string]int{},
		hi:     map[string]store.Extreme{},
		lo:     map[string]store.Extreme{},
		last:   map[string]float64{},
	}
}

func (b *bucket) add(ts int64, vals map[string]float64) {
	for name, v := range vals {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		o, ok := wx.Lookup(name)
		if !ok {
			continue
		}
		switch name {
		case wx.WindGust:
			if !b.hasG || v > b.gust {
				b.gust, b.gustAt, b.hasG = v, ts, true
				if d, ok := vals[wx.WindDir]; ok {
					b.gustD = &d
				}
			}
			continue
		case wx.WindDir:
			if s, ok := vals[wx.WindSpeed]; ok && s > 0 {
				rad := v * math.Pi / 180
				b.xsum += s * math.Sin(rad)
				b.ysum += s * math.Cos(rad)
			}
			b.last[name] = v
			continue
		}
		switch o.Agg {
		case wx.AggSum:
			b.sums[name] += v
			b.counts[name] = 1
		case wx.AggMax:
			if cur, ok := b.last[name]; !ok || v > cur {
				b.last[name] = v
			}
		case wx.AggLast, wx.AggDir:
			b.last[name] = v
		case wx.AggAvg:
			b.sums[name] += v
			b.counts[name]++
			if e, ok := b.hi[name]; !ok || v > e.Value {
				b.hi[name] = store.Extreme{Value: v, Time: ts}
			}
			if e, ok := b.lo[name]; !ok || v < e.Value {
				b.lo[name] = store.Extreme{Value: v, Time: ts}
			}
		}
	}
}

func (b *bucket) record(intervalMin int, src store.Source) (store.Record, bool) {
	vals := map[string]float64{}
	for name, s := range b.sums {
		o, _ := wx.Lookup(name)
		if o.Agg == wx.AggSum {
			vals[name] = s
		} else if b.counts[name] > 0 {
			vals[name] = s / float64(b.counts[name])
		}
	}
	for name, v := range b.last {
		vals[name] = v
	}
	if b.xsum != 0 || b.ysum != 0 {
		vals[wx.WindDir] = math.Mod(math.Atan2(b.xsum, b.ysum)*180/math.Pi+360, 360)
	}
	hi := b.hi
	if b.hasG {
		vals[wx.WindGust] = b.gust
		hi[wx.WindGust] = store.Extreme{Value: b.gust, Time: b.gustAt}
		if b.gustD != nil {
			vals[wx.WindGustDir] = *b.gustD
		}
	}
	if len(vals) == 0 {
		return store.Record{}, false
	}
	return store.Record{DateTime: b.end, Interval: intervalMin, Source: src, Values: vals, Hi: hi, Lo: b.lo}, true
}

type Bucketer struct {
	interval int64
	src      store.Source
	cur      *bucket
	out      []store.Record
}

func NewBucketer(intervalSecs int64, src store.Source) *Bucketer {
	return &Bucketer{interval: intervalSecs, src: src}
}

func (b *Bucketer) Add(ts int64, vals map[string]float64) {
	end := ((ts + b.interval - 1) / b.interval) * b.interval
	if b.cur == nil || b.cur.end != end {
		b.flush()
		b.cur = newBucket(end)
	}
	b.cur.add(ts, vals)
}

func (b *Bucketer) flush() {
	if b.cur == nil {
		return
	}
	if r, ok := b.cur.record(int(b.interval/60), b.src); ok {
		b.out = append(b.out, r)
	}
	b.cur = nil
}

func (b *Bucketer) Drain(final bool) []store.Record {
	if final {
		b.flush()
	}
	out := b.out
	b.out = nil
	return out
}

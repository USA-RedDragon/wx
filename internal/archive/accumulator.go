package archive

import (
	"math"
	"sync"

	"github.com/USA-RedDragon/wx/internal/store"
	"github.com/USA-RedDragon/wx/internal/wx"
)

const rainRateWindow = 15 * 60

type rainEvent struct {
	ts     int64
	amount float64
}

type Accumulator struct {
	mu       sync.Mutex
	interval int64
	start    int64
	sums     map[string]float64
	counts   map[string]int
	last     map[string]float64
	gust     float64
	gustDir  *float64
	hasGust  bool
	xsum     float64
	ysum     float64
	rain     float64
	hasRain  bool
	lightCnt *float64
	strikes  float64
	hasLight bool
	rainLog  []rainEvent
	current  map[string]float64
	curTime  int64
}

func NewAccumulator(intervalSecs int64) *Accumulator {
	a := &Accumulator{interval: intervalSecs, current: map[string]float64{}}
	a.reset(0)
	return a
}

func (a *Accumulator) reset(start int64) {
	a.start = start
	a.sums = map[string]float64{}
	a.counts = map[string]int{}
	a.last = map[string]float64{}
	a.gust, a.gustDir, a.hasGust = 0, nil, false
	a.xsum, a.ysum = 0, 0
	a.rain, a.hasRain = 0, false
	a.strikes, a.hasLight = 0, false
}

func (a *Accumulator) boundary(ts int64) int64 {
	return (ts / a.interval) * a.interval
}

func (a *Accumulator) Current() (map[string]float64, int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make(map[string]float64, len(a.current))
	for k, v := range a.current {
		out[k] = v
	}
	return out, a.curTime
}

func (a *Accumulator) AddPacket(ts int64, values map[string]float64) []store.Record {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := a.roll(ts)
	if a.start == 0 {
		a.start = a.boundary(ts)
	}
	a.curTime = ts
	for name, v := range values {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		a.current[name] = v
		o, ok := wx.Lookup(name)
		if !ok {
			continue
		}
		switch name {
		case wx.Rain:
			continue
		case wx.LightningStrikeCount:
			if a.lightCnt != nil && v >= *a.lightCnt {
				a.strikes += v - *a.lightCnt
			}
			a.lightCnt = &v
			a.hasLight = true
			continue
		case wx.WindGust:
			if !a.hasGust || v > a.gust {
				a.gust, a.hasGust = v, true
				if d, ok := values[wx.WindDir]; ok {
					a.gustDir = &d
				}
			}
			continue
		case wx.WindDir:
			if s, ok := values[wx.WindSpeed]; ok && s > 0 {
				rad := v * math.Pi / 180
				a.xsum += s * math.Sin(rad)
				a.ysum += s * math.Cos(rad)
			}
			a.last[name] = v
			continue
		}
		switch o.Agg {
		case wx.AggLast, wx.AggDir:
			a.last[name] = v
		case wx.AggMax:
			if cur, ok := a.last[name]; !ok || v > cur {
				a.last[name] = v
			}
		case wx.AggAvg, wx.AggSum:
			a.sums[name] += v
			a.counts[name]++
		}
	}
	return out
}

func (a *Accumulator) AddRain(ts int64, inches float64) []store.Record {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := a.roll(ts)
	if a.start == 0 {
		a.start = a.boundary(ts)
	}
	if inches > 0 {
		a.rain += inches
		a.rainLog = append(a.rainLog, rainEvent{ts, inches})
	}
	a.hasRain = true
	return out
}

func (a *Accumulator) Tick(now int64) []store.Record {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.roll(now)
}

func (a *Accumulator) roll(now int64) []store.Record {
	if a.start == 0 || now < a.start+a.interval {
		return nil
	}
	end := a.start + a.interval
	rec, ok := a.build(end)
	a.reset(a.boundary(now))
	if !ok {
		return nil
	}
	return []store.Record{rec}
}

func (a *Accumulator) build(end int64) (store.Record, bool) {
	vals := map[string]float64{}
	for name, s := range a.sums {
		if a.counts[name] > 0 {
			vals[name] = s / float64(a.counts[name])
		}
	}
	for name, v := range a.last {
		vals[name] = v
	}
	if a.xsum != 0 || a.ysum != 0 {
		vals[wx.WindDir] = math.Mod(math.Atan2(a.xsum, a.ysum)*180/math.Pi+360, 360)
	}
	if a.hasGust {
		vals[wx.WindGust] = a.gust
		if a.gustDir != nil {
			vals[wx.WindGustDir] = *a.gustDir
		}
	}
	if a.hasRain {
		vals[wx.Rain] = a.rain
		cutoff := end - rainRateWindow
		kept := a.rainLog[:0]
		var window float64
		for _, e := range a.rainLog {
			if e.ts > cutoff {
				kept = append(kept, e)
				if e.ts <= end {
					window += e.amount
				}
			}
		}
		a.rainLog = kept
		vals[wx.RainRate] = window * 3600 / rainRateWindow
	}
	if a.hasLight {
		vals[wx.LightningStrikeCount] = a.strikes
	}
	if len(vals) == 0 {
		return store.Record{}, false
	}
	return store.Record{DateTime: end, Interval: int(a.interval / 60), Source: store.SourceLive, Values: vals}, true
}

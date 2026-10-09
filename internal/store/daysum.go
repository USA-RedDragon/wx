package store

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"time"

	"github.com/USA-RedDragon/wx/internal/wx"
)

type DaySum struct {
	Obs        string
	Day        int64
	Min        *float64
	MinTime    int64
	Max        *float64
	MaxTime    int64
	Sum        float64
	Count      int64
	WSum       float64
	SumTime    int64
	LastTime   int64
	MaxDir     *float64
	XSum       float64
	YSum       float64
	WSquareSum float64
}

type dayKey struct {
	obs string
	day int64
}

type DayAccumulator struct {
	loc  *time.Location
	days map[dayKey]*DaySum
}

func NewDayAccumulator(loc *time.Location) *DayAccumulator {
	return &DayAccumulator{loc: loc, days: map[dayKey]*DaySum{}}
}

func DayStart(ts int64, loc *time.Location) int64 {
	t := time.Unix(ts-1, 0).In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc).Unix()
}

func (a *DayAccumulator) get(obs string, day int64) *DaySum {
	k := dayKey{obs, day}
	d, ok := a.days[k]
	if !ok {
		d = &DaySum{Obs: obs, Day: day}
		a.days[k] = d
	}
	return d
}

func ptr(v float64) *float64 { return &v }

func (d *DaySum) observe(v float64, weight int64, ts int64, lo, hi *Extreme) {
	minV, minT := v, ts
	if lo != nil {
		minV, minT = lo.Value, lo.Time
	}
	maxV, maxT := v, ts
	if hi != nil {
		maxV, maxT = hi.Value, hi.Time
	}
	if d.Min == nil || minV < *d.Min {
		d.Min, d.MinTime = ptr(minV), minT
	}
	if d.Max == nil || maxV > *d.Max {
		d.Max, d.MaxTime = ptr(maxV), maxT
	}
	d.Sum += v
	d.Count++
	d.WSum += v * float64(weight)
	d.SumTime += weight
	if ts > d.LastTime {
		d.LastTime = ts
	}
}

func (a *DayAccumulator) Add(r Record) {
	day := DayStart(r.DateTime, a.loc)
	weight := int64(r.Interval) * 60
	if weight <= 0 {
		weight = 60
	}
	for name, v := range r.Values {
		if math.IsNaN(v) {
			continue
		}
		var lo, hi *Extreme
		if e, ok := r.Lo[name]; ok {
			lo = &e
		}
		if e, ok := r.Hi[name]; ok {
			hi = &e
		}
		a.get(name, day).observe(v, weight, r.DateTime, lo, hi)
	}
	speed, okS := r.Values[wx.WindSpeed]
	if !okS {
		return
	}
	w := a.get("wind", day)
	maxV, maxT := speed, r.DateTime
	if e, ok := r.Hi[wx.WindSpeed]; ok {
		maxV, maxT = e.Value, e.Time
	}
	var maxDir *float64
	if g, ok := r.Values[wx.WindGust]; ok && g >= maxV {
		maxV = g
		if e, ok := r.Hi[wx.WindGust]; ok {
			maxV, maxT = e.Value, e.Time
		}
		if gd, ok := r.Values[wx.WindGustDir]; ok {
			maxDir = ptr(gd)
		} else if wd, ok := r.Values[wx.WindDir]; ok {
			maxDir = ptr(wd)
		}
	} else if wd, ok := r.Values[wx.WindDir]; ok {
		maxDir = ptr(wd)
	}
	if w.Max == nil || maxV > *w.Max {
		w.Max, w.MaxTime, w.MaxDir = ptr(maxV), maxT, maxDir
	}
	if w.Min == nil || speed < *w.Min {
		w.Min, w.MinTime = ptr(speed), r.DateTime
	}
	w.Sum += speed
	w.Count++
	w.WSum += speed * float64(weight)
	w.SumTime += weight
	w.WSquareSum += speed * speed * float64(weight)
	if r.DateTime > w.LastTime {
		w.LastTime = r.DateTime
	}
	if dir, ok := r.Values[wx.WindDir]; ok && speed > 0 {
		rad := dir * math.Pi / 180
		w.XSum += speed * math.Sin(rad) * float64(weight)
		w.YSum += speed * math.Cos(rad) * float64(weight)
	}
}

func (a *DayAccumulator) Len() int { return len(a.days) }

const upsertDay = `INSERT INTO archive_day (obs, day, min, mintime, max, maxtime, sum, count, wsum, sumtime, lasttime, maxdir, xsum, ysum, wsquaresum)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(obs, day) DO UPDATE SET
	mintime = CASE WHEN excluded.min IS NOT NULL AND (archive_day.min IS NULL OR excluded.min < archive_day.min) THEN excluded.mintime ELSE archive_day.mintime END,
	min = CASE WHEN excluded.min IS NOT NULL AND (archive_day.min IS NULL OR excluded.min < archive_day.min) THEN excluded.min ELSE archive_day.min END,
	maxdir = CASE WHEN excluded.max IS NOT NULL AND (archive_day.max IS NULL OR excluded.max > archive_day.max) THEN excluded.maxdir ELSE archive_day.maxdir END,
	maxtime = CASE WHEN excluded.max IS NOT NULL AND (archive_day.max IS NULL OR excluded.max > archive_day.max) THEN excluded.maxtime ELSE archive_day.maxtime END,
	max = CASE WHEN excluded.max IS NOT NULL AND (archive_day.max IS NULL OR excluded.max > archive_day.max) THEN excluded.max ELSE archive_day.max END,
	sum = archive_day.sum + excluded.sum,
	count = archive_day.count + excluded.count,
	wsum = archive_day.wsum + excluded.wsum,
	sumtime = archive_day.sumtime + excluded.sumtime,
	lasttime = max(coalesce(archive_day.lasttime, 0), coalesce(excluded.lasttime, 0)),
	xsum = archive_day.xsum + excluded.xsum,
	ysum = archive_day.ysum + excluded.ysum,
	wsquaresum = archive_day.wsquaresum + excluded.wsquaresum`

func mergeDaysTx(ctx context.Context, tx execer, acc *DayAccumulator) error {
	if acc.Len() == 0 {
		return nil
	}
	st, err := tx.PrepareContext(ctx, upsertDay)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	for _, d := range acc.days {
		if _, err := st.ExecContext(ctx, d.Obs, d.Day, d.Min, d.MinTime, d.Max, d.MaxTime, d.Sum, d.Count, d.WSum, d.SumTime, d.LastTime, d.MaxDir, d.XSum, d.YSum, d.WSquareSum); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) MergeDays(ctx context.Context, acc *DayAccumulator) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := mergeDaysTx(ctx, tx, acc); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.bump()
	return nil
}

func (s *Store) AddLive(ctx context.Context, r Record, loc *time.Location) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	n, err := s.insertTx(ctx, tx, []Record{r}, false)
	if err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	acc := NewDayAccumulator(loc)
	acc.Add(r)
	if err := mergeDaysTx(ctx, tx, acc); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.bump()
	return nil
}

func (s *Store) RebuildDays(ctx context.Context, loc *time.Location) error {
	lo, hi, err := s.Bounds(ctx)
	if err != nil {
		return err
	}
	return s.RebuildDaysBetween(ctx, loc, lo, hi)
}

func (s *Store) RebuildDaysBetween(ctx context.Context, loc *time.Location, from, to int64) error {
	const chunkDays = 30
	first := DayStart(from, loc)
	last := DayStart(to, loc)
	for day := time.Unix(first, 0).In(loc); day.Unix() <= last; day = day.AddDate(0, 0, chunkDays) {
		end := day.AddDate(0, 0, chunkDays)
		if err := s.rebuildChunk(ctx, loc, day.Unix(), end.Unix()); err != nil {
			return err
		}
	}
	s.bump()
	return nil
}

func (s *Store) rebuildChunk(ctx context.Context, loc *time.Location, startDay, endDay int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM archive_day WHERE day >= ? AND day < ?`, startDay, endDay); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`SELECT %s FROM archive WHERE "dateTime" > ? AND "dateTime" <= ? ORDER BY "dateTime"`, s.selectCols()), startDay, endDay)
	if err != nil {
		return err
	}
	recs, err := s.scanRecords(rows)
	if err != nil {
		return err
	}
	acc := NewDayAccumulator(loc)
	for _, r := range recs {
		acc.Add(r)
	}
	if err := mergeDaysTx(ctx, tx, acc); err != nil {
		return err
	}
	return tx.Commit()
}

type Stats struct {
	Obs      string
	Min      *float64
	MinTime  int64
	Max      *float64
	MaxTime  int64
	Sum      *float64
	Avg      *float64
	Count    int64
	LastTime int64
	MaxDir   *float64
	RMS      *float64
	VecAvg   *float64
	VecDir   *float64
}

func (s *Store) Stats(ctx context.Context, obs string, startDay, endDay int64) (Stats, error) {
	st := Stats{Obs: obs}
	row := s.db.QueryRowContext(ctx, `SELECT
		(SELECT min FROM archive_day WHERE obs = ?1 AND day >= ?2 AND day < ?3 AND min IS NOT NULL ORDER BY min ASC, mintime ASC LIMIT 1),
		(SELECT mintime FROM archive_day WHERE obs = ?1 AND day >= ?2 AND day < ?3 AND min IS NOT NULL ORDER BY min ASC, mintime ASC LIMIT 1),
		(SELECT max FROM archive_day WHERE obs = ?1 AND day >= ?2 AND day < ?3 AND max IS NOT NULL ORDER BY max DESC, maxtime ASC LIMIT 1),
		(SELECT maxtime FROM archive_day WHERE obs = ?1 AND day >= ?2 AND day < ?3 AND max IS NOT NULL ORDER BY max DESC, maxtime ASC LIMIT 1),
		(SELECT maxdir FROM archive_day WHERE obs = ?1 AND day >= ?2 AND day < ?3 AND max IS NOT NULL ORDER BY max DESC, maxtime ASC LIMIT 1),
		sum(sum), sum(count), sum(wsum), sum(sumtime), max(lasttime), sum(xsum), sum(ysum), sum(wsquaresum)
		FROM archive_day WHERE obs = ?1 AND day >= ?2 AND day < ?3`, obs, startDay, endDay)
	var minV, maxV, maxDir, sum, wsum, xsum, ysum, wsq sql.NullFloat64
	var minT, maxT, count, sumtime, last sql.NullInt64
	if err := row.Scan(&minV, &minT, &maxV, &maxT, &maxDir, &sum, &count, &wsum, &sumtime, &last, &xsum, &ysum, &wsq); err != nil {
		return st, fmt.Errorf("stats %s: %w", obs, err)
	}
	if !count.Valid || count.Int64 == 0 {
		return st, nil
	}
	st.Count = count.Int64
	st.LastTime = last.Int64
	if minV.Valid {
		st.Min, st.MinTime = ptr(minV.Float64), minT.Int64
	}
	if maxV.Valid {
		st.Max, st.MaxTime = ptr(maxV.Float64), maxT.Int64
	}
	if maxDir.Valid {
		st.MaxDir = ptr(maxDir.Float64)
	}
	st.Sum = ptr(sum.Float64)
	if sumtime.Int64 > 0 {
		st.Avg = ptr(wsum.Float64 / float64(sumtime.Int64))
		st.RMS = ptr(math.Sqrt(wsq.Float64 / float64(sumtime.Int64)))
		st.VecAvg = ptr(math.Hypot(xsum.Float64, ysum.Float64) / float64(sumtime.Int64))
		if xsum.Float64 != 0 || ysum.Float64 != 0 {
			d := math.Mod(math.Atan2(xsum.Float64, ysum.Float64)*180/math.Pi+360, 360)
			st.VecDir = ptr(d)
		}
	}
	return st, nil
}

func (s *Store) HasData(ctx context.Context, obs string, sinceDay int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM archive_day WHERE obs = ? AND day >= ? AND count > 0)`, obs, sinceDay).Scan(&n)
	return n == 1, err
}

func (s *Store) Months(ctx context.Context, loc *time.Location) ([]time.Time, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT day FROM archive_day WHERE obs = 'outTemp' OR obs = 'inTemp' ORDER BY day`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	seen := map[string]bool{}
	var out []time.Time
	for rows.Next() {
		var d int64
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		t := time.Unix(d, 0).In(loc)
		k := t.Format("2006-01")
		if !seen[k] {
			seen[k] = true
			out = append(out, t)
		}
	}
	return out, rows.Err()
}

func KnownObs(name string) bool {
	if name == "wind" {
		return true
	}
	_, ok := wx.Lookup(name)
	return ok
}

func (s *Store) Days(ctx context.Context, obs string, startDay, endDay int64) ([]DaySum, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT day, min, mintime, max, maxtime, sum, count, wsum, sumtime, coalesce(lasttime, 0), maxdir, xsum, ysum, wsquaresum
		FROM archive_day WHERE obs = ? AND day >= ? AND day < ? AND count > 0 ORDER BY day`, obs, startDay, endDay)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []DaySum
	for rows.Next() {
		d := DaySum{Obs: obs}
		var minV, maxV, maxDir sql.NullFloat64
		var minT, maxT sql.NullInt64
		if err := rows.Scan(&d.Day, &minV, &minT, &maxV, &maxT, &d.Sum, &d.Count, &d.WSum, &d.SumTime, &d.LastTime, &maxDir, &d.XSum, &d.YSum, &d.WSquareSum); err != nil {
			return nil, err
		}
		if minV.Valid {
			d.Min, d.MinTime = ptr(minV.Float64), minT.Int64
		}
		if maxV.Valid {
			d.Max, d.MaxTime = ptr(maxV.Float64), maxT.Int64
		}
		if maxDir.Valid {
			d.MaxDir = ptr(maxDir.Float64)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (d DaySum) Avg() *float64 {
	if d.SumTime <= 0 {
		return nil
	}
	return ptr(d.WSum / float64(d.SumTime))
}

func (d DaySum) VecDir() *float64 {
	if d.XSum == 0 && d.YSum == 0 {
		return nil
	}
	return ptr(math.Mod(math.Atan2(d.XSum, d.YSum)*180/math.Pi+360, 360))
}

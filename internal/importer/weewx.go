package importer

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"

	"github.com/USA-RedDragon/wx/internal/store"
	"github.com/USA-RedDragon/wx/internal/wx"
)

func weewxSkip() map[string]bool {
	return map[string]bool{wx.LightningStrikeCount: true, wx.LightningDistance: true, wx.LightningEnergy: true}
}

type Result struct {
	Source   string
	Read     int64
	Inserted int64
	First    int64
	Last     int64
}

func (r Result) String() string {
	return fmt.Sprintf("%s: read %d, inserted %d, %d..%d", r.Source, r.Read, r.Inserted, r.First, r.Last)
}

func ImportWeewxSQLite(ctx context.Context, st *store.Store, path string, intervalSecs int64, elevFt float64) (Result, error) {
	res := Result{Source: "weewx " + path}
	src, err := sql.Open("sqlite", "file:"+path+"?mode=ro&immutable=1")
	if err != nil {
		return res, err
	}
	defer func() { _ = src.Close() }()
	have := map[string]bool{}
	rows, err := src.QueryContext(ctx, `SELECT name FROM pragma_table_info('archive')`)
	if err != nil {
		return res, err
	}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			_ = rows.Close()
			return res, err
		}
		have[n] = true
	}
	_ = rows.Close()
	if !have["dateTime"] || !have["usUnits"] {
		return res, fmt.Errorf("%s has no weewx archive table", path)
	}
	skip := weewxSkip()
	var cols []string
	for _, n := range wx.Names() {
		if have[n] && !skip[n] {
			cols = append(cols, n)
		}
	}
	q := make([]string, 0, len(cols))
	for _, c := range cols {
		q = append(q, fmt.Sprintf("%q", c))
	}
	rows, err = src.QueryContext(ctx, fmt.Sprintf(`SELECT "dateTime", "usUnits", %s FROM archive ORDER BY "dateTime"`, strings.Join(q, ",")))
	if err != nil {
		return res, err
	}
	defer func() { _ = rows.Close() }()
	b := NewBucketer(intervalSecs, store.SourceWeewx)
	vals := make([]sql.NullFloat64, len(cols))
	dest := make([]any, 2+len(cols))
	var ts float64
	var units int
	dest[0], dest[1] = &ts, &units
	for i := range vals {
		dest[2+i] = &vals[i]
	}
	var pending []store.Record
	reducer := wx.PressureReducer{ElevFt: elevFt}
	flush := func(final bool) error {
		for _, r := range b.Drain(final) {
			reducer.Apply(r.DateTime, r.Values)
			pending = append(pending, r)
		}
		if len(pending) < 5000 && !final {
			return nil
		}
		n, err := st.InsertRecords(ctx, pending, false)
		res.Inserted += n
		pending = pending[:0]
		return err
	}
	for rows.Next() {
		if err := rows.Scan(dest...); err != nil {
			return res, err
		}
		if units != wx.USUnits {
			return res, fmt.Errorf("record %d uses usUnits %d, only US (1) is supported", int64(ts), units)
		}
		t := int64(ts)
		if res.First == 0 {
			res.First = t
		}
		res.Last = t
		res.Read++
		m := make(map[string]float64, len(cols))
		for i, c := range cols {
			if vals[i].Valid {
				m[c] = vals[i].Float64
			}
		}
		b.Add(t, m)
		if err := flush(false); err != nil {
			return res, err
		}
	}
	if err := rows.Err(); err != nil {
		return res, err
	}
	if err := flush(true); err != nil {
		return res, err
	}
	slog.Info("imported weewx archive", "result", res.String())
	return res, nil
}

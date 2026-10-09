package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/USA-RedDragon/wx/internal/wx"
)

type Source int

const (
	SourceLive Source = iota
	SourceWeewx
	SourcePrometheus
	SourceHAShortTerm
	SourceHAHourly
)

func (s Source) String() string {
	switch s {
	case SourceLive:
		return "live"
	case SourceWeewx:
		return "weewx"
	case SourcePrometheus:
		return "prometheus"
	case SourceHAShortTerm:
		return "homeassistant-5m"
	case SourceHAHourly:
		return "homeassistant-hourly"
	}
	return "unknown"
}

type Extreme struct {
	Value float64
	Time  int64
}

type Record struct {
	DateTime int64
	Interval int
	Source   Source
	Values   map[string]float64
	Hi       map[string]Extreme
	Lo       map[string]Extreme
}

func (r Record) Get(name string) *float64 {
	if v, ok := r.Values[name]; ok {
		return &v
	}
	return nil
}

type Store struct {
	db      *sql.DB
	columns []string
	mu      sync.Mutex
	gen     uint64
}

var ErrNoData = errors.New("no data")

const colDateTime = `"dateTime"`

func Open(ctx context.Context, path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	s := &Store{db: db, columns: wx.Names()}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) DB() *sql.DB {
	return s.db
}

func (s *Store) Generation() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gen
}

func (s *Store) bump() {
	s.mu.Lock()
	s.gen++
	s.mu.Unlock()
}

func (s *Store) migrate(ctx context.Context) error {
	cols := make([]string, 0, len(s.columns))
	for _, c := range s.columns {
		cols = append(cols, fmt.Sprintf("%q REAL", c))
	}
	stmts := []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS archive (
			"dateTime" INTEGER NOT NULL PRIMARY KEY,
			"usUnits" INTEGER NOT NULL,
			"interval" INTEGER NOT NULL,
			"source" INTEGER NOT NULL DEFAULT 0,
			%s
		) WITHOUT ROWID`, strings.Join(cols, ",\n")),
		`CREATE TABLE IF NOT EXISTS archive_day (
			obs TEXT NOT NULL,
			day INTEGER NOT NULL,
			min REAL, mintime INTEGER,
			max REAL, maxtime INTEGER,
			sum REAL NOT NULL DEFAULT 0,
			count INTEGER NOT NULL DEFAULT 0,
			wsum REAL NOT NULL DEFAULT 0,
			sumtime INTEGER NOT NULL DEFAULT 0,
			lasttime INTEGER,
			maxdir REAL,
			xsum REAL NOT NULL DEFAULT 0,
			ysum REAL NOT NULL DEFAULT 0,
			wsquaresum REAL NOT NULL DEFAULT 0,
			PRIMARY KEY (obs, day)
		) WITHOUT ROWID`,
		`CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
	}
	for _, st := range stmts {
		if _, err := s.db.ExecContext(ctx, st); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	existing := map[string]bool{}
	rows, err := s.db.QueryContext(ctx, `SELECT name FROM pragma_table_info('archive')`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			_ = rows.Close()
			return err
		}
		existing[n] = true
	}
	_ = rows.Close()
	for _, c := range s.columns {
		if !existing[c] {
			if _, err := s.db.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE archive ADD COLUMN %q REAL`, c)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) insertSQL(replace bool) string {
	verb := "INSERT OR IGNORE"
	if replace {
		verb = "INSERT OR REPLACE"
	}
	cols := make([]string, 0, 4+len(s.columns))
	cols = append(cols, colDateTime, `"usUnits"`, `"interval"`, `"source"`)
	ph := make([]string, 0, 4+len(s.columns))
	ph = append(ph, "?", "?", "?", "?")
	for _, c := range s.columns {
		cols = append(cols, fmt.Sprintf("%q", c))
		ph = append(ph, "?")
	}
	return fmt.Sprintf("%s INTO archive (%s) VALUES (%s)", verb, strings.Join(cols, ","), strings.Join(ph, ","))
}

func (s *Store) args(r Record) []any {
	a := make([]any, 0, 4+len(s.columns))
	a = append(a, r.DateTime, wx.USUnits, r.Interval, int(r.Source))
	for _, c := range s.columns {
		if v, ok := r.Values[c]; ok {
			a = append(a, v)
		} else {
			a = append(a, nil)
		}
	}
	return a
}

type execer interface {
	PrepareContext(ctx context.Context, query string) (*sql.Stmt, error)
}

func (s *Store) insertTx(ctx context.Context, tx execer, recs []Record, replace bool) (int64, error) {
	st, err := tx.PrepareContext(ctx, s.insertSQL(replace))
	if err != nil {
		return 0, err
	}
	defer func() { _ = st.Close() }()
	var n int64
	for _, r := range recs {
		res, err := st.ExecContext(ctx, s.args(r)...)
		if err != nil {
			return n, err
		}
		c, _ := res.RowsAffected()
		n += c
	}
	return n, nil
}

func (s *Store) InsertRecords(ctx context.Context, recs []Record, replace bool) (int64, error) {
	if len(recs) == 0 {
		return 0, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	n, err := s.insertTx(ctx, tx, recs, replace)
	if err != nil {
		return n, err
	}
	if err := tx.Commit(); err != nil {
		return n, err
	}
	s.bump()
	return n, nil
}

func (s *Store) scanRecords(rows *sql.Rows) ([]Record, error) {
	defer func() { _ = rows.Close() }()
	var out []Record
	n := 4 + len(s.columns)
	vals := make([]sql.NullFloat64, len(s.columns))
	dest := make([]any, n)
	var dt int64
	var units, interval, source int
	dest[0], dest[1], dest[2], dest[3] = &dt, &units, &interval, &source
	for i := range vals {
		dest[4+i] = &vals[i]
	}
	for rows.Next() {
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		r := Record{DateTime: dt, Interval: interval, Source: Source(source), Values: map[string]float64{}}
		for i, c := range s.columns {
			if vals[i].Valid {
				r.Values[c] = vals[i].Float64
			}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) selectCols() string {
	cols := make([]string, 0, 4+len(s.columns))
	cols = append(cols, colDateTime, `"usUnits"`, `"interval"`, `"source"`)
	for _, c := range s.columns {
		cols = append(cols, fmt.Sprintf("%q", c))
	}
	return strings.Join(cols, ",")
}

func (s *Store) Range(ctx context.Context, start, end int64) ([]Record, error) {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`SELECT %s FROM archive WHERE "dateTime" > ? AND "dateTime" <= ? ORDER BY "dateTime"`, s.selectCols()), start, end)
	if err != nil {
		return nil, err
	}
	return s.scanRecords(rows)
}

func (s *Store) Latest(ctx context.Context) (Record, error) {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`SELECT %s FROM archive ORDER BY "dateTime" DESC LIMIT 1`, s.selectCols()))
	if err != nil {
		return Record{}, err
	}
	recs, err := s.scanRecords(rows)
	if err != nil {
		return Record{}, err
	}
	if len(recs) == 0 {
		return Record{}, ErrNoData
	}
	return recs[0], nil
}

func (s *Store) Bounds(ctx context.Context) (int64, int64, error) {
	var lo, hi sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT min("dateTime"), max("dateTime") FROM archive`).Scan(&lo, &hi); err != nil {
		return 0, 0, err
	}
	if !lo.Valid {
		return 0, 0, ErrNoData
	}
	return lo.Int64, hi.Int64, nil
}

func (s *Store) Covered(ctx context.Context, start, end int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM archive WHERE "dateTime" > ? AND "dateTime" <= ?)`, start, end).Scan(&n)
	return n == 1, err
}

func (s *Store) LastSeen(ctx context.Context, obs string, since int64) (int64, bool, error) {
	if _, ok := wx.Lookup(obs); !ok {
		return 0, false, fmt.Errorf("unknown observation %q", obs)
	}
	var t sql.NullInt64
	err := s.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT max("dateTime") FROM archive WHERE "dateTime" > ? AND %q IS NOT NULL`, obs), since).Scan(&t)
	if err != nil {
		return 0, false, err
	}
	return t.Int64, t.Valid, nil
}

func (s *Store) SetMeta(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO meta(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func (s *Store) Meta(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func (s *Store) Each(ctx context.Context, cols []string, start, end int64, fn func(Record)) error {
	q := []string{colDateTime, `"interval"`}
	valid := make([]string, 0, len(cols))
	for _, c := range cols {
		if _, ok := wx.Lookup(c); ok {
			q = append(q, fmt.Sprintf("%q", c))
			valid = append(valid, c)
		}
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`SELECT %s FROM archive WHERE "dateTime" > ? AND "dateTime" <= ? ORDER BY "dateTime"`, strings.Join(q, ",")), start, end)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	vals := make([]sql.NullFloat64, len(valid))
	dest := make([]any, 2+len(valid))
	var dt int64
	var interval int
	dest[0], dest[1] = &dt, &interval
	for i := range vals {
		dest[2+i] = &vals[i]
	}
	r := Record{Values: make(map[string]float64, len(valid))}
	for rows.Next() {
		if err := rows.Scan(dest...); err != nil {
			return err
		}
		clear(r.Values)
		r.DateTime, r.Interval = dt, interval
		for i, c := range valid {
			if vals[i].Valid {
				r.Values[c] = vals[i].Float64
			}
		}
		fn(r)
	}
	return rows.Err()
}

func (s *Store) ValueNear(ctx context.Context, obs string, ts, grace int64) (float64, bool, error) {
	if _, ok := wx.Lookup(obs); !ok {
		return 0, false, fmt.Errorf("unknown observation %q", obs)
	}
	var v sql.NullFloat64
	err := s.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT %q FROM archive WHERE "dateTime" BETWEEN ? AND ? AND %q IS NOT NULL ORDER BY abs("dateTime" - ?) LIMIT 1`, obs, obs), ts-grace, ts+grace, ts).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return v.Float64, v.Valid, nil
}

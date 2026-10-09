package importer

import (
	"context"
	"time"

	"github.com/USA-RedDragon/wx/internal/store"
)

const mergeChunk = int64(7 * 24 * time.Hour / time.Second)

func ImportWxDatabase(ctx context.Context, st *store.Store, path string) (Result, error) {
	total := Result{Source: "wx " + path}
	src, err := store.Open(ctx, path)
	if err != nil {
		return total, err
	}
	defer func() { _ = src.Close() }()
	lo, hi, err := src.Bounds(ctx)
	if err != nil {
		return total, err
	}
	for from := lo - 1; from < hi; from += mergeChunk {
		recs, err := src.Range(ctx, from, from+mergeChunk)
		if err != nil {
			return total, err
		}
		res, err := ImportRecordsGapFill(ctx, st, recs, total.Source)
		total.Add(res)
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

package importer

import (
	"context"

	"github.com/USA-RedDragon/wx/internal/store"
)

func ImportWxDatabase(ctx context.Context, st *store.Store, path string) (Result, error) {
	src, err := store.Open(ctx, path)
	if err != nil {
		return Result{Source: "wx " + path}, err
	}
	defer func() { _ = src.Close() }()
	lo, hi, err := src.Bounds(ctx)
	if err != nil {
		return Result{Source: "wx " + path}, err
	}
	recs, err := src.Range(ctx, lo-1, hi)
	if err != nil {
		return Result{Source: "wx " + path}, err
	}
	return ImportRecordsGapFill(ctx, st, recs, "wx "+path)
}

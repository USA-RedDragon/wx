package ingest_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/USA-RedDragon/configulator/v2"
	"github.com/USA-RedDragon/wx/internal/config"
	"github.com/USA-RedDragon/wx/internal/ingest"
	"github.com/USA-RedDragon/wx/internal/store"
	_ "modernc.org/sqlite"
)

func TestHandleIgnoresGarbageAndTracksRain(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	if err != nil {
		t.Fatal(err)
	}
	cfg.MQTT.RainTopic = "rtl_433/Cotech"
	cfg.Station.AltitudeFeet = 1191
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "wx.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	in := ingest.New(&cfg, st)
	ctx := context.Background()
	in.Handle(ctx, cfg.MQTT.Topic, []byte(`garbage`))
	in.Handle(ctx, cfg.MQTT.Topic, []byte(`{"outTemp": 21.5, "rain": 99}`))
	in.Handle(ctx, "rtl_433/Cotech", []byte(`{"rain_mm": 100}`))
	in.Handle(ctx, "rtl_433/Cotech", []byte(`{"rain_mm": 102.54}`))
	cur, _ := in.Accumulator().Current()
	if cur["outTemp"] < 70.6 || cur["outTemp"] > 70.8 {
		t.Errorf("outTemp %v", cur["outTemp"])
	}
	v, err := st.Meta(ctx, "rain_baseline_mm")
	if err != nil || v == "" {
		t.Errorf("rain baseline not persisted: %q %v", v, err)
	}
}

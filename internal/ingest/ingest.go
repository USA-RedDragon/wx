package ingest

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/USA-RedDragon/wx/internal/archive"
	"github.com/USA-RedDragon/wx/internal/config"
	"github.com/USA-RedDragon/wx/internal/store"
	"github.com/USA-RedDragon/wx/internal/wx"
	"github.com/eclipse/paho.golang/autopaho"
	"github.com/eclipse/paho.golang/paho"
)

const (
	metaRainBaseline = "rain_baseline_mm"
	rainBaselineTTL  = 24 * 60 * 60
	maxClockSkew     = 300
)

type Listener interface {
	Record(r store.Record)
	Packet(ctx context.Context, ts int64, values map[string]float64)
}

type Ingest struct {
	listener Listener
	cfg      *config.Config
	store    *store.Store
	acc      *archive.Accumulator
	rain     archive.RainTracker
	rainMu   sync.Mutex
	cm       *autopaho.ConnectionManager
	nowFunc  func() time.Time
	reducer  wx.PressureReducer
}

func New(cfg *config.Config, st *store.Store) *Ingest {
	return &Ingest{
		cfg:     cfg,
		store:   st,
		acc:     archive.NewAccumulator(int64(cfg.Station.ArchiveSecs)),
		nowFunc: time.Now,
		reducer: wx.PressureReducer{ElevFt: cfg.Station.AltitudeFeet},
	}
}

func (i *Ingest) SetListener(l Listener) {
	i.listener = l
}

func (i *Ingest) Accumulator() *archive.Accumulator {
	return i.acc
}

func (i *Ingest) loadRainBaseline(ctx context.Context) {
	v, err := i.store.Meta(ctx, metaRainBaseline)
	if err != nil || v == "" {
		return
	}
	parts := strings.SplitN(v, "@", 2)
	if len(parts) != 2 {
		return
	}
	mm, err1 := strconv.ParseFloat(parts[0], 64)
	ts, err2 := strconv.ParseInt(parts[1], 10, 64)
	if err1 != nil || err2 != nil || i.nowFunc().Unix()-ts > rainBaselineTTL {
		return
	}
	i.rain.Set(mm)
}

func (i *Ingest) saveRainBaseline(ctx context.Context) {
	mm, ok := i.rain.Value()
	if !ok {
		return
	}
	v := fmt.Sprintf("%g@%d", mm, i.nowFunc().Unix())
	if err := i.store.SetMeta(ctx, metaRainBaseline, v); err != nil {
		slog.Warn("failed to persist rain baseline", "error", err)
	}
}

func (i *Ingest) Start(ctx context.Context) error {
	i.loadRainBaseline(ctx)
	u, err := url.Parse(i.cfg.MQTT.Broker)
	if err != nil {
		return fmt.Errorf("parse broker URL: %w", err)
	}
	subs := []paho.SubscribeOptions{{Topic: i.cfg.MQTT.Topic, QoS: 0}}
	if i.cfg.MQTT.RainTopic != "" {
		subs = append(subs, paho.SubscribeOptions{Topic: i.cfg.MQTT.RainTopic, QoS: 0})
	}
	pc := autopaho.ClientConfig{
		ServerUrls:                    []*url.URL{u},
		KeepAlive:                     30,
		CleanStartOnInitialConnection: true,
		ConnectUsername:               i.cfg.MQTT.Username,
		ConnectPassword:               []byte(i.cfg.MQTT.Password),
		OnConnectionUp: func(cm *autopaho.ConnectionManager, _ *paho.Connack) {
			slog.Info("MQTT connected", "broker", i.cfg.MQTT.Broker)
			if _, err := cm.Subscribe(ctx, &paho.Subscribe{Subscriptions: subs}); err != nil && ctx.Err() == nil {
				slog.Error("MQTT subscribe failed", "error", err)
			}
		},
		OnConnectError: func(err error) {
			slog.Warn("MQTT connection error", "error", err)
		},
		ClientConfig: paho.ClientConfig{
			ClientID: i.cfg.MQTT.ClientID,
			OnPublishReceived: []func(paho.PublishReceived) (bool, error){
				func(pr paho.PublishReceived) (bool, error) {
					i.Handle(ctx, pr.Packet.Topic, pr.Packet.Payload)
					return true, nil
				},
			},
		},
	}
	cm, err := autopaho.NewConnection(ctx, pc)
	if err != nil {
		return err
	}
	i.cm = cm
	go i.tick(ctx)
	return nil
}

func (i *Ingest) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	i.persist(ctx, i.acc.Tick(i.nowFunc().Unix()))
	i.saveRainBaseline(ctx)
	if i.cm == nil {
		return nil
	}
	return i.cm.Disconnect(ctx)
}

func (i *Ingest) tick(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			i.persist(ctx, i.acc.Tick(now.Unix()))
		}
	}
}

func (i *Ingest) packetTime(ts int64) int64 {
	now := i.nowFunc().Unix()
	if ts <= 0 || ts > now+maxClockSkew || ts < now-maxClockSkew {
		return now
	}
	return ts
}

func (i *Ingest) Handle(ctx context.Context, topic string, payload []byte) {
	switch topic {
	case i.cfg.MQTT.Topic:
		p, err := archive.ParseStationPacket(payload)
		if err != nil {
			slog.Debug("bad station packet", "error", err)
			return
		}
		ts := i.packetTime(p.DateTime)
		i.persist(ctx, i.acc.AddPacket(ts, p.Values))
		if i.listener != nil {
			i.listener.Packet(ctx, ts, p.Values)
		}
		if p.RainMM != nil && i.cfg.MQTT.RainTopic == "" {
			i.addRain(ctx, ts, *p.RainMM)
		}
	case i.cfg.MQTT.RainTopic:
		mm, err := archive.ParseRainCounter(payload)
		if err != nil {
			slog.Debug("bad rain packet", "error", err)
			return
		}
		i.addRain(ctx, i.nowFunc().Unix(), mm)
	}
}

func (i *Ingest) addRain(ctx context.Context, ts int64, totalMM float64) {
	i.rainMu.Lock()
	_, had := i.rain.Value()
	d := i.rain.Update(totalMM)
	i.rainMu.Unlock()
	i.persist(ctx, i.acc.AddRain(ts, wx.MmToIn(d)))
	if d > 0 || !had {
		i.saveRainBaseline(ctx)
	}
}

func (i *Ingest) persist(ctx context.Context, recs []store.Record) {
	for _, r := range recs {
		i.reducer.Apply(r.DateTime, r.Values)
		if err := i.store.AddLive(ctx, r, i.cfg.Station.Timezone); err != nil {
			slog.Error("failed to store archive record", "error", err, "dateTime", r.DateTime)
			continue
		}
		slog.Debug("archived record", "dateTime", time.Unix(r.DateTime, 0), "fields", len(r.Values))
		if i.listener != nil {
			i.listener.Record(r)
		}
	}
}

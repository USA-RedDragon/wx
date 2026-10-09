package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"sync"
	"time"

	"github.com/USA-RedDragon/wx/internal/store"
	"github.com/USA-RedDragon/wx/internal/wx"
)

const (
	heartbeatInterval = 25 * time.Second
	packetThrottle    = 5 * time.Second
	maxLiveClients    = 200
	clientBuffer      = 8
	retryMillis       = 5000
	eventRecord       = "record"
	eventCurrent      = "current"
)

type livePacket struct {
	mu   sync.Mutex
	last time.Time
}

type recordEvent struct {
	DateTime int64 `json:"dateTime"`
}

type currentEvent struct {
	DateTime int64  `json:"dateTime"`
	Current  string `json:"current"`
}

func encodeEvent(name string, v any) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return fmt.Appendf(nil, "event: %s\ndata: %s\n\n", name, data), nil
}

func (s *Server) Record(r store.Record) {
	if s.hub.Count() == 0 {
		return
	}
	msg, err := encodeEvent(eventRecord, recordEvent{DateTime: r.DateTime})
	if err != nil {
		return
	}
	s.hub.Broadcast(msg)
}

func (s *Server) Packet(ctx context.Context, ts int64, values map[string]float64) {
	if s.hub.Count() == 0 {
		return
	}
	s.packet.mu.Lock()
	now := time.Now()
	if now.Sub(s.packet.last) < packetThrottle {
		s.packet.mu.Unlock()
		return
	}
	s.packet.last = now
	s.packet.mu.Unlock()
	html, err := s.renderCurrent(ctx, values)
	if err != nil {
		if !errors.Is(err, store.ErrNoData) {
			slog.Debug("render live current conditions", "error", err)
		}
		return
	}
	msg, err := encodeEvent(eventCurrent, currentEvent{DateTime: ts, Current: html})
	if err != nil {
		return
	}
	s.hub.Broadcast(msg)
}

func (s *Server) renderCurrent(ctx context.Context, values map[string]float64) (string, error) {
	pc, err := s.context(ctx)
	if err != nil {
		return "", err
	}
	merged := make(map[string]float64, len(pc.latest.Values)+len(values))
	maps.Copy(merged, pc.latest.Values)
	maps.Copy(merged, values)
	if _, ok := values[wx.Pressure]; ok {
		delete(merged, wx.Barometer)
		wx.DerivePressure(merged, s.cfg.Station.AltitudeFeet)
	}
	pc.latest.Values = merged
	rows, err := s.currentRows(ctx, pc)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&b, "current-rows", rows); err != nil {
		return "", err
	}
	return b.String(), nil
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	c, err := s.hub.Subscribe()
	if err != nil {
		http.Error(w, "too many live clients", http.StatusServiceUnavailable)
		return
	}
	defer s.hub.Unsubscribe(c)
	_ = rc.SetWriteDeadline(time.Time{})
	h := w.Header()
	h.Set(headerCType, "text/event-stream")
	h.Set(headerCache, "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprintf(w, "retry: %d\n\n", retryMillis); err != nil {
		return
	}
	if err := rc.Flush(); err != nil {
		return
	}
	ticker := time.NewTicker(s.heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-c.Done():
			return
		case msg := <-c.Messages():
			if _, err := w.Write(msg); err != nil {
				return
			}
		case <-ticker.C:
			if _, err := w.Write([]byte(": ping\n\n")); err != nil {
				return
			}
		}
		if err := rc.Flush(); err != nil {
			return
		}
	}
}

func (s *Server) LiveClients() int {
	return s.hub.Count()
}

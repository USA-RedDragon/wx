package web_test

import (
	"bufio"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/USA-RedDragon/wx/internal/store"
	"github.com/USA-RedDragon/wx/internal/wx"
)

func readEvent(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	var b strings.Builder
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("read: %v (so far %q)", err, b.String())
		}
		if line == "\n" {
			return b.String()
		}
		b.WriteString(line)
	}
}

func TestEvents(t *testing.T) {
	t.Parallel()
	ts, srv := setupServer(t)
	srv.SetHeartbeat(200 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("content type %q", ct)
	}
	if resp.Header.Get("Cache-Control") != "no-cache" || resp.Header.Get("X-Accel-Buffering") != "no" {
		t.Errorf("headers %v", resp.Header)
	}
	r := bufio.NewReader(resp.Body)
	if ev := readEvent(t, r); !strings.HasPrefix(ev, "retry: ") {
		t.Errorf("first event %q", ev)
	}
	for srv.LiveClients() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	srv.Record(store.Record{DateTime: 1732608120})
	if ev := readEvent(t, r); ev != "event: record\ndata: {\"dateTime\":1732608120}\n" {
		t.Errorf("record event %q", ev)
	}
	srv.Packet(ctx, 1732608125, map[string]float64{wx.OutTemp: 77.7, wx.Pressure: 28.7})
	ev := readEvent(t, r)
	if !strings.HasPrefix(ev, "event: current\n") || !strings.Contains(ev, "77.7°F") || !strings.Contains(ev, "Barometer") {
		t.Errorf("current event %q", ev)
	}
	srv.Packet(ctx, 1732608126, map[string]float64{wx.OutTemp: 78})
	if ev := readEvent(t, r); ev != ": ping\n" {
		t.Errorf("expected throttled packet and a heartbeat, got %q", ev)
	}
	cancel()
	deadline := time.Now().Add(5 * time.Second)
	for srv.LiveClients() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := srv.LiveClients(); n != 0 {
		t.Errorf("client not released after disconnect: %d", n)
	}
}

func TestNoClientsIsFree(t *testing.T) {
	t.Parallel()
	_, srv := setupServer(t)
	srv.Record(store.Record{DateTime: 1})
	srv.Packet(context.Background(), 1, map[string]float64{wx.OutTemp: 1})
	if srv.LiveClients() != 0 {
		t.Error("unexpected clients")
	}
}

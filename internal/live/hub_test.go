package live_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/USA-RedDragon/wx/internal/live"
)

func TestFanOut(t *testing.T) {
	t.Parallel()
	h := live.NewHub(10, 4)
	a, _ := h.Subscribe()
	b, _ := h.Subscribe()
	if n := h.Broadcast([]byte("x")); n != 2 {
		t.Fatalf("sent to %d", n)
	}
	for _, c := range []*live.Client{a, b} {
		if got := string(<-c.Messages()); got != "x" {
			t.Errorf("got %q", got)
		}
	}
	h.Unsubscribe(a)
	h.Unsubscribe(a)
	if h.Count() != 1 {
		t.Errorf("count %d", h.Count())
	}
	select {
	case <-a.Done():
	default:
		t.Error("unsubscribed client not done")
	}
}

func TestSlowClientIsDropped(t *testing.T) {
	t.Parallel()
	h := live.NewHub(10, 2)
	slow, _ := h.Subscribe()
	fast, _ := h.Subscribe()
	for range 3 {
		h.Broadcast([]byte("m"))
		<-fast.Messages()
	}
	select {
	case <-slow.Done():
	default:
		t.Fatal("slow client was not dropped")
	}
	if h.Count() != 1 {
		t.Errorf("count %d", h.Count())
	}
}

func TestLimitAndClose(t *testing.T) {
	t.Parallel()
	h := live.NewHub(2, 1)
	a, _ := h.Subscribe()
	if _, err := h.Subscribe(); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Subscribe(); !errors.Is(err, live.ErrTooManyClients) {
		t.Errorf("err %v", err)
	}
	h.Close()
	<-a.Done()
	if h.Count() != 0 {
		t.Errorf("count %d", h.Count())
	}
}

func TestConcurrent(t *testing.T) {
	t.Parallel()
	h := live.NewHub(100, 8)
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			c, err := h.Subscribe()
			if err != nil {
				return
			}
			go h.Broadcast([]byte("y"))
			h.Unsubscribe(c)
		})
	}
	wg.Wait()
	if h.Count() != 0 {
		t.Errorf("count %d", h.Count())
	}
}

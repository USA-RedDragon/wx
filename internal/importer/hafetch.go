package importer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/USA-RedDragon/wx/internal/wx"
	"github.com/coder/websocket"
)

const (
	haChunk        = 31 * 24 * time.Hour
	haReadLimit    = 256 << 20
	haDialTimeout  = 30 * time.Second
	haTokenEnvHint = "HA_TOKEN"
)

var ErrHAAuth = errors.New("home assistant rejected the token")

func HAStatisticIDs() []string {
	seen := map[string]bool{haRainEntity: true}
	ids := []string{haRainEntity}
	for _, em := range haEntities() {
		for _, e := range em.Entities {
			if !seen[e] {
				seen[e] = true
				ids = append(ids, e)
			}
		}
	}
	return ids
}

type haMessage struct {
	ID      int             `json:"id"`
	Type    string          `json:"type"`
	Success bool            `json:"success"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func haWebsocketURL(base string) (string, error) {
	u, err := url.Parse(strings.TrimSuffix(base, "/"))
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	}
	u.Path += "/api/websocket"
	return u.String(), nil
}

func FetchHAStatistics(ctx context.Context, base, token string, start, end time.Time) (map[string][]HAStat, error) {
	out := map[string][]HAStat{}
	err := StreamHAStatistics(ctx, base, token, start, end, func(part map[string][]HAStat) error {
		for k, v := range part {
			out[k] = append(out[k], v...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func StreamHAStatistics(ctx context.Context, base, token string, start, end time.Time, fn func(map[string][]HAStat) error) error {
	if token == "" {
		return fmt.Errorf("%s is empty", haTokenEnvHint)
	}
	wsURL, err := haWebsocketURL(base)
	if err != nil {
		return err
	}
	dialCtx, cancel := context.WithTimeout(ctx, haDialTimeout)
	defer cancel()
	conn, _, err := websocket.Dial(dialCtx, wsURL, nil)
	if err != nil {
		return err
	}
	defer func() { _ = conn.CloseNow() }()
	conn.SetReadLimit(haReadLimit)
	read := func() (haMessage, error) {
		var m haMessage
		_, b, err := conn.Read(ctx)
		if err != nil {
			return m, err
		}
		return m, json.Unmarshal(b, &m)
	}
	write := func(v any) error {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		return conn.Write(ctx, websocket.MessageText, b)
	}
	if _, err := read(); err != nil {
		return err
	}
	if err := write(map[string]string{"type": "auth", "access_token": token}); err != nil {
		return err
	}
	auth, err := read()
	if err != nil {
		return err
	}
	if auth.Type != "auth_ok" {
		return ErrHAAuth
	}
	id := 0
	for from := start; from.Before(end); from = from.Add(haChunk) {
		to := from.Add(haChunk)
		if to.After(end) {
			to = end
		}
		id++
		req := map[string]any{
			"id":            id,
			"type":          "recorder/statistics_during_period",
			"start_time":    from.UTC().Format(time.RFC3339),
			"end_time":      to.UTC().Format(time.RFC3339),
			"statistic_ids": HAStatisticIDs(),
			"period":        "hour",
			"types":         []string{"mean", "min", "max", "sum", "state", "change"},
			"units":         map[string]string{"temperature": "°F", wx.Pressure: "inHg", "distance": "in", "speed": "mph"},
		}
		if err := write(req); err != nil {
			return err
		}
		for {
			m, err := read()
			if err != nil {
				return err
			}
			if m.ID != id {
				continue
			}
			if !m.Success {
				msg := "unknown error"
				if m.Error != nil {
					msg = m.Error.Message
				}
				return fmt.Errorf("statistics_during_period: %s", msg)
			}
			part := map[string][]HAStat{}
			if err := json.Unmarshal(m.Result, &part); err != nil {
				return err
			}
			if err := fn(part); err != nil {
				return err
			}
			break
		}
	}
	return nil
}

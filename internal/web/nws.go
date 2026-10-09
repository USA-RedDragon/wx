package web

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

type ForecastPeriod struct {
	Name             string    `json:"name"`
	StartTime        time.Time `json:"startTime"`
	Temperature      float64   `json:"temperature"`
	TemperatureUnit  string    `json:"temperatureUnit"`
	TemperatureTrend string    `json:"temperatureTrend"`
	WindSpeed        string    `json:"windSpeed"`
	WindDirection    string    `json:"windDirection"`
	Icon             string    `json:"icon"`
	ShortForecast    string    `json:"shortForecast"`
	DetailedForecast string    `json:"detailedForecast"`
}

type Alert struct {
	Headline    string `json:"headline"`
	Event       string `json:"event"`
	Severity    string `json:"severity"`
	Effective   string `json:"effective"`
	Onset       string `json:"onset"`
	Ends        string `json:"ends"`
	Expires     string `json:"expires"`
	Status      string `json:"status"`
	Certainty   string `json:"certainty"`
	Urgency     string `json:"urgency"`
	Description string `json:"description"`
	Instruction string `json:"instruction"`
	SenderName  string `json:"senderName"`
}

type Forecast struct {
	TwelveHour  []ForecastPeriod
	Hourly      []ForecastPeriod
	Alerts      []Alert
	GeneratedAt time.Time
}

type NWS struct {
	client    *http.Client
	userAgent string
	lat, lng  float64
	interval  time.Duration
	mu        sync.RWMutex
	current   *Forecast
	loc       *time.Location
}

func NewNWS(userAgent string, lat, lng float64, interval time.Duration, loc *time.Location) *NWS {
	return &NWS{client: &http.Client{Timeout: 15 * time.Second}, userAgent: userAgent, lat: lat, lng: lng, interval: interval, loc: loc}
}

func (n *NWS) Current() *Forecast {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.current
}

func (n *NWS) get(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", n.userAgent)
	req.Header.Set("Accept", "application/geo+json")
	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (n *NWS) refresh(ctx context.Context) error {
	var pt struct {
		Properties struct {
			Forecast       string `json:"forecast"`
			ForecastHourly string `json:"forecastHourly"`
		} `json:"properties"`
	}
	if err := n.get(ctx, fmt.Sprintf("https://api.weather.gov/points/%.4f,%.4f", n.lat, n.lng), &pt); err != nil {
		return err
	}
	type periods struct {
		Properties struct {
			Periods []ForecastPeriod `json:"periods"`
		} `json:"properties"`
	}
	var twelve, hourly periods
	if err := n.get(ctx, pt.Properties.Forecast, &twelve); err != nil {
		return err
	}
	if err := n.get(ctx, pt.Properties.ForecastHourly, &hourly); err != nil {
		return err
	}
	var alerts struct {
		Features []struct {
			Properties Alert `json:"properties"`
		} `json:"features"`
	}
	if err := n.get(ctx, fmt.Sprintf("https://api.weather.gov/alerts/active?point=%.4f,%.4f", n.lat, n.lng), &alerts); err != nil {
		slog.Warn("NWS alerts fetch failed", "error", err)
	}
	f := &Forecast{TwelveHour: twelve.Properties.Periods, Hourly: hourly.Properties.Periods, GeneratedAt: time.Now()}
	for _, a := range alerts.Features {
		f.Alerts = append(f.Alerts, a.Properties)
	}
	for i := range f.Hourly {
		f.Hourly[i].Icon = strings.Replace(strings.Replace(f.Hourly[i].Icon, "?size=small", "?size=medium", 1), ",0?", "?", 1)
	}
	n.mu.Lock()
	n.current = f
	n.mu.Unlock()
	return nil
}

func (n *NWS) Run(ctx context.Context) {
	for {
		wait := n.interval
		if err := n.refresh(ctx); err != nil {
			slog.Warn("NWS forecast fetch failed", "error", err)
			wait = 5 * time.Minute
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

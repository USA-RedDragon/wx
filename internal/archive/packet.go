package archive

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/USA-RedDragon/wx/internal/wx"
)

type RainTracker struct {
	prev  float64
	valid bool
}

func (t *RainTracker) Update(total float64) float64 {
	if math.IsNaN(total) {
		return 0
	}
	if !t.valid {
		t.prev, t.valid = total, true
		return 0
	}
	d := total - t.prev
	t.prev = total
	if d < 0 {
		return 0
	}
	return d
}

func (t *RainTracker) Set(total float64) {
	t.prev, t.valid = total, true
}

func (t *RainTracker) Value() (float64, bool) {
	return t.prev, t.valid
}

type Packet struct {
	DateTime int64
	Values   map[string]float64
	RainMM   *float64
}

func ParseStationPacket(payload []byte) (Packet, error) {
	raw := map[string]any{}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return Packet{}, fmt.Errorf("decode packet: %w", err)
	}
	p := Packet{Values: map[string]float64{}}
	for k, v := range raw {
		f, ok := v.(float64)
		if !ok {
			continue
		}
		switch k {
		case "dateTime":
			p.DateTime = int64(f)
		case "usUnits":
		case wx.Rain:
			mm := f
			p.RainMM = &mm
		case wx.Barometer, wx.Pressure:
			p.Values[wx.Pressure] = wx.FromMetricWX(wx.Pressure, f)
		case wx.Altimeter:
		default:
			if _, known := wx.Lookup(k); known {
				p.Values[k] = wx.FromMetricWX(k, f)
			}
		}
	}
	return p, nil
}

func ParseRainCounter(payload []byte) (float64, error) {
	var m struct {
		RainMM *float64 `json:"rain_mm"`
	}
	if err := json.Unmarshal(payload, &m); err != nil {
		return 0, fmt.Errorf("decode rain packet: %w", err)
	}
	if m.RainMM == nil {
		return 0, fmt.Errorf("rain packet has no rain_mm")
	}
	return *m.RainMM, nil
}

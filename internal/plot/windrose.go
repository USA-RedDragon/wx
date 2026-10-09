package plot

import (
	"bytes"
	"fmt"
	"html"
	"math"

	"github.com/USA-RedDragon/wx/internal/store"
	"github.com/USA-RedDragon/wx/internal/wx"
)

func petalColors() []string {
	return []string{"#0859ec", "#5e3ffb", "#9d4ce8", "#c96ab8", "#d98175", "#cf8e2a", "#b4930f"}
}

func speedBins() []float64 {
	return []float64{0, 2, 4, 6, 10, 14, 20}
}

const (
	roseW      = 600
	roseH      = 580
	roseBorder = 20
	ringColor  = "#ddd9c3"
)

func WindRose(recs []store.Record, title, stamp string) []byte {
	petalColors, speedBins := petalColors(), speedBins()
	var counts [16][7]int
	var total, calm int
	for _, r := range recs {
		s, ok := r.Values[wx.WindSpeed]
		if !ok || math.IsNaN(s) {
			continue
		}
		total++
		d, okD := r.Values[wx.WindDir]
		if s <= 0 || !okD {
			calm++
			continue
		}
		sector := int(math.Floor(math.Mod(d+11.25, 360)/22.5)) % 16
		bin := 0
		for i := len(speedBins) - 1; i >= 0; i-- {
			if s >= speedBins[i] {
				bin = i
				break
			}
		}
		counts[sector][bin]++
	}
	var binTotals [7]int
	maxPct := 0.0
	for s := range 16 {
		sum := 0
		for b := range 7 {
			sum += counts[s][b]
			binTotals[b] += counts[s][b]
		}
		if total > 0 {
			maxPct = math.Max(maxPct, float64(sum)/float64(total)*100)
		}
	}
	ringStep := math.Max(1, math.Ceil(maxPct/5))
	maxRing := ringStep * 5
	cx, cy := 220.0, 300.0
	radius := 180.0
	var b bytes.Buffer
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="%d" height="%d" font-family="FreeSans, sans-serif" font-weight="bold">`, roseW, roseH, roseW, roseH)
	b.WriteString(`<rect width="100%" height="100%" fill="#101010"/>`)
	fmt.Fprintf(&b, `<text x="%d" y="34" fill="#eeeeee" font-size="20" text-anchor="middle">%s</text>`, roseW/2, html.EscapeString(title))
	for i := 1; i <= 5; i++ {
		r := radius * float64(i) / 5
		fmt.Fprintf(&b, `<circle cx="%.1f" cy="%.1f" r="%.1f" fill="none" stroke="%s" stroke-width="1"/>`, cx, cy, r, ringColor)
		fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" fill="#eeeeee" font-size="12">%.0f%%</text>`, cx+r*0.7+4, cy+r*0.7+4, ringStep*float64(i))
	}
	fmt.Fprintf(&b, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s"/>`, cx-radius-10, cy, cx+radius+10, cy, ringColor)
	fmt.Fprintf(&b, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s"/>`, cx, cy-radius-10, cx, cy+radius+10, ringColor)
	for _, l := range []struct {
		t    string
		x, y float64
	}{{"N", cx, cy - radius - 14}, {"S", cx, cy + radius + 28}, {"E", cx + radius + 22, cy + 7}, {"W", cx - radius - 22, cy + 7}} {
		fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" fill="#eeeeee" font-size="20" text-anchor="middle">%s</text>`, l.x, l.y, l.t)
	}
	if total > 0 {
		half := 22.5 / 2 * 0.8
		for s := 15; s >= 0; s-- {
			cum := 0
			for bin := range 7 {
				cum += counts[s][bin]
			}
			for bin := 6; bin >= 0; bin-- {
				if cum == 0 {
					break
				}
				r := float64(cum) / float64(total) * 100 / maxRing * radius
				ang := float64(s) * 22.5
				a1 := (ang - half - 90) * math.Pi / 180
				a2 := (ang + half - 90) * math.Pi / 180
				fmt.Fprintf(&b, `<path d="M%.1f %.1f L%.1f %.1f A%.1f %.1f 0 0 1 %.1f %.1f Z" fill="%s" stroke="#101010" stroke-width="1"/>`,
					cx, cy, cx+r*math.Cos(a1), cy+r*math.Sin(a1), r, r, cx+r*math.Cos(a2), cy+r*math.Sin(a2), petalColors[bin])
				cum -= counts[s][bin]
			}
		}
	}
	calmPct := 0.0
	if total > 0 {
		calmPct = float64(calm) / float64(total) * 100
	}
	fmt.Fprintf(&b, `<circle cx="%.1f" cy="%.1f" r="12" fill="%s"/>`, cx, cy, petalColors[0])
	fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" fill="#eeeeee" font-size="11" text-anchor="middle">%.0f%%</text>`, cx, cy+4, calmPct)
	lx := 470.0
	fmt.Fprintf(&b, `<text x="%.1f" y="90" fill="#eeeeee" font-size="14" text-anchor="middle">Wind Speed</text><text x="%.1f" y="106" fill="#eeeeee" font-size="14" text-anchor="middle">(mph)</text>`, lx, lx)
	top, bottom := 120.0, 460.0
	segH := (bottom - top) / float64(len(speedBins))
	for i := len(speedBins) - 1; i >= 0; i-- {
		y := top + float64(len(speedBins)-1-i)*segH
		fmt.Fprintf(&b, `<rect x="%.1f" y="%.1f" width="20" height="%.1f" fill="%s"/>`, lx-10, y, segH, petalColors[i])
		pct := 0.0
		if total > 0 {
			pct = float64(binTotals[i]) / float64(total) * 100
		}
		fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" fill="#eeeeee" font-size="14">%.0f (%.0f%%)</text>`, lx+16, y+5, speedBins[i], pct)
	}
	fmt.Fprintf(&b, `<circle cx="%.1f" cy="%.1f" r="10" fill="%s"/><text x="%.1f" y="%.1f" fill="#eeeeee" font-size="14" text-anchor="end">Calm</text>`, lx, bottom+10, petalColors[0], lx-16, bottom+15)
	fmt.Fprintf(&b, `<text x="%.1f" y="%d" fill="#eeeeee" font-size="14" text-anchor="middle">%s</text>`, lx, roseH-30, html.EscapeString(stamp))
	b.WriteString(`</svg>`)
	return b.Bytes()
}

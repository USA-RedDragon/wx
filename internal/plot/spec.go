package plot

import (
	"time"

	"github.com/USA-RedDragon/wx/internal/wx"
)

type Kind int

const (
	Line Kind = iota
	Bar
	Vector
	Markers
)

type Agg int

const (
	AggNone Agg = iota
	AggAvg
	AggSum
	AggMax
	AggVecDir
)

type Series struct {
	Obs   string
	Label string
	Agg   Agg
	AggI  time.Duration
}

type YScale struct {
	Min, Max, Step *float64
}

type Spec struct {
	Group  string
	Kind   Kind
	Series []Series
	YScale YScale
}

type Period struct {
	Name     string
	Length   time.Duration
	AggI     time.Duration
	Agg      Agg
	DayNight bool
	XFormat  string
}

const (
	PeriodDay   = "day"
	PeriodWeek  = "week"
	PeriodMonth = "month"
	PeriodYear  = "year"
)

func f(v float64) *float64 { return &v }

func Periods() []Period {
	return []Period{
		{Name: PeriodDay, Length: 27 * time.Hour, Agg: AggNone, DayNight: true, XFormat: "03:04 PM MST"},
		{Name: PeriodWeek, Length: 7 * 24 * time.Hour, AggI: time.Hour, Agg: AggAvg, DayNight: true, XFormat: "02"},
		{Name: PeriodMonth, Length: 30 * 24 * time.Hour, AggI: 3 * time.Hour, Agg: AggAvg, XFormat: "02"},
		{Name: PeriodYear, Length: 365 * 24 * time.Hour, AggI: 24 * time.Hour, Agg: AggAvg, XFormat: "01/02"},
	}
}

func PeriodByName(name string) (Period, bool) {
	for _, p := range Periods() {
		if p.Name == name {
			return p, true
		}
	}
	return Period{}, false
}

func Groups() []string {
	return []string{"tempdew", "tempfeel", "hum", wx.Barometer, "wind", "winddir", "windvec", wx.Rain, "ET", wx.UV, wx.Radiation, "tempin", wx.Cloudbase, "lightning", "pm", "signalout"}
}

func Specs(period string) map[string]Spec {
	rainAgg := map[string]time.Duration{PeriodDay: time.Hour, PeriodWeek: 24 * time.Hour, PeriodMonth: 24 * time.Hour, PeriodYear: 7 * 24 * time.Hour}[period]
	rainLabel := map[string]string{PeriodDay: "Rain (hourly total)", PeriodWeek: "Rain (daily total)", PeriodMonth: "Rain (daily total)", PeriodYear: "Rain (weekly total)"}[period]
	lightLabel := map[string]string{PeriodDay: "Lightning (hourly total)", PeriodWeek: "Lightning (daily total)", PeriodMonth: "Lightning (daily total)", PeriodYear: "Lightning (weekly total)"}[period]
	hum := []Series{{Obs: wx.OutHumidity}, {Obs: wx.InHumidity}}
	if period != PeriodDay {
		hum = []Series{{Obs: wx.OutHumidity}}
	}
	feel := []Series{{Obs: wx.OutTemp}, {Obs: wx.HeatIndex}, {Obs: wx.WindChill}}
	if period != PeriodDay {
		feel = []Series{{Obs: wx.WindChill}, {Obs: wx.HeatIndex}}
	}
	wind := []Series{{Obs: wx.WindSpeed}, {Obs: wx.WindGust}}
	if period != PeriodDay {
		wind = []Series{{Obs: wx.WindSpeed}, {Obs: wx.WindGust, Agg: AggMax}}
	}
	winddir := []Series{{Obs: wx.WindDir}}
	if period != PeriodDay {
		winddir = []Series{{Obs: wx.WindDir, Agg: AggVecDir}}
	}
	m := map[string]Spec{
		"tempdew":    {Series: []Series{{Obs: wx.OutTemp}, {Obs: wx.Dewpoint}, {Obs: wx.Frostpoint}}},
		"tempfeel":   {Series: feel},
		"hum":        {Series: hum},
		"humin":      {Series: []Series{{Obs: wx.InHumidity}}},
		wx.Barometer: {Series: []Series{{Obs: wx.Barometer}}},
		"wind":       {Series: wind},
		"winddir":    {Kind: Markers, Series: winddir, YScale: YScale{f(0), f(360), f(45)}},
		"windvec":    {Kind: Vector, Series: []Series{{Obs: wx.WindSpeed, Label: "Wind Vector"}}},
		wx.Rain:      {Kind: Bar, Series: []Series{{Obs: wx.Rain, Label: rainLabel, Agg: AggSum, AggI: rainAgg}}, YScale: YScale{Step: f(0.02)}},
		"ET":         {Kind: Bar, Series: []Series{{Obs: "ET", Label: "Evapotranspiration", Agg: AggSum, AggI: rainAgg}}, YScale: YScale{Step: f(0.02)}},
		wx.UV:        {Series: []Series{{Obs: wx.UV}}},
		wx.Radiation: {Series: []Series{{Obs: wx.Radiation}}},
		"tempin":     {Series: []Series{{Obs: wx.InTemp}}},
		wx.Cloudbase: {Series: []Series{{Obs: wx.Cloudbase}}},
		"lightning":  {Kind: Bar, Series: []Series{{Obs: wx.LightningStrikeCount, Label: lightLabel, Agg: AggSum, AggI: rainAgg}}, YScale: YScale{Step: f(1)}},
		"pm":         {Series: []Series{{Obs: wx.PM1}, {Obs: wx.PM25}}},
		"signalout":  {Series: []Series{{Obs: wx.OutRSSI}, {Obs: wx.OutSNR}, {Obs: wx.OutNoise}}},
		wx.OutRSSI:   {Series: []Series{{Obs: wx.OutRSSI}}},
		wx.OutSNR:    {Series: []Series{{Obs: wx.OutSNR}}},
		wx.OutNoise:  {Series: []Series{{Obs: wx.OutNoise}}},
		"inRSSI":     {Series: []Series{{Obs: "inRSSI"}}},
		"inSNR":      {Series: []Series{{Obs: "inSNR"}}},
		"inNoise":    {Series: []Series{{Obs: "inNoise"}}},
		"rx":         {Series: []Series{{Obs: "rxCheckPercent"}}, YScale: YScale{f(0), f(100), f(25)}},
		"volt":       {Series: []Series{{Obs: "consBatteryVoltage"}, {Obs: "heatingVoltage"}, {Obs: "supplyVoltage"}, {Obs: "referenceVoltage"}}},
	}
	for k, v := range m {
		v.Group = k
		m[k] = v
	}
	return m
}

func Obs(spec Spec) []string {
	out := make([]string, 0, len(spec.Series))
	for _, s := range spec.Series {
		out = append(out, s.Obs)
	}
	return out
}

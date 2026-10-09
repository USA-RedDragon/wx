package wx

import (
	"fmt"
	"math"
)

type Agg int

const (
	AggAvg Agg = iota
	AggSum
	AggMax
	AggLast
	AggDir
)

type Obs struct {
	Name   string
	Label  string
	Unit   string
	Format string
	Agg    Agg
}

const (
	fmt0      = "%.0f"
	fmt1      = "%.1f"
	fmt2      = "%.2f"
	fmt3      = "%.3f"
	fmtDir    = "%03.0f"
	unitF     = "°F"
	unitInHg  = " inHg"
	unitMph   = " mph"
	unitDeg   = "°"
	unitDB    = " dB"
	unitPct   = "%"
	unitPM    = " µg/m³"
	unitPPM   = " ppm"
	notApplic = "N/A"
)

func Observations() []Obs {
	return []Obs{
		{OutTemp, "Outside Temperature", unitF, fmt1, AggAvg},
		{HeatIndex, "Heat Index", unitF, fmt1, AggAvg},
		{WindChill, "Wind Chill", unitF, fmt1, AggAvg},
		{Dewpoint, "Dew Point", unitF, fmt1, AggAvg},
		{Frostpoint, "Frostpoint", unitF, fmt1, AggAvg},
		{OutHumidity, "Outside Humidity", unitPct, fmt0, AggAvg},
		{Barometer, "Barometer", unitInHg, fmt3, AggAvg},
		{Pressure, "Pressure", unitInHg, fmt3, AggAvg},
		{Altimeter, "Altimeter", unitInHg, fmt3, AggAvg},
		{WindSpeed, "Wind Speed", unitMph, fmt0, AggAvg},
		{WindDir, "Wind Direction", unitDeg, fmtDir, AggDir},
		{WindGust, "Gust Speed", unitMph, fmt0, AggMax},
		{WindGustDir, "Gust Direction", unitDeg, fmtDir, AggLast},
		{Rain, "Rain", " in", fmt2, AggSum},
		{RainRate, "Rain Rate", " in/h", fmt2, AggMax},
		{UV, "UV Index", "", fmt1, AggAvg},
		{Radiation, "Radiation", " W/m²", fmt0, AggAvg},
		{Luminosity, "Luminosity", " lux", fmt0, AggAvg},
		{Cloudbase, "Cloudbase", " feet", fmt0, AggAvg},
		{LightningStrikeCount, "Lightning Strikes", "", fmt0, AggSum},
		{LightningDistance, "Lightning Distance", " miles", fmt1, AggAvg},
		{LightningEnergy, "Lightning Energy", "", fmt0, AggAvg},
		{InTemp, "Inside Temperature", unitF, fmt1, AggAvg},
		{InHumidity, "Inside Humidity", unitPct, fmt0, AggAvg},
		{PM1, "Particles (1µm)", unitPM, fmt0, AggAvg},
		{PM25, "Particles (2.5µm)", unitPM, fmt0, AggAvg},
		{CO2, "eCO2", unitPPM, fmt0, AggAvg},
		{TVOC, "Total Volatile Organic Compounds", unitPPM, "%.4f", AggAvg},
		{OutRSSI, "Weather Station RSSI", unitDB, fmt0, AggAvg},
		{OutSNR, "Weather Station SNR", unitDB, fmt0, AggAvg},
		{OutNoise, "Weather Station Noise", unitDB, fmt0, AggAvg},
		{OutTempBattery, "Weather Station Battery Status", "", fmt0, AggLast},
	}
}

func Names() []string {
	obs := Observations()
	out := make([]string, 0, len(obs))
	for _, o := range obs {
		out = append(out, o.Name)
	}
	return out
}

func Lookup(name string) (Obs, bool) {
	for _, o := range Observations() {
		if o.Name == name {
			return o, true
		}
	}
	return Obs{}, false
}

func Label(name string) string {
	if o, ok := Lookup(name); ok {
		return o.Label
	}
	return name
}

func UnitLabel(name string) string {
	if o, ok := Lookup(name); ok {
		return o.Unit
	}
	return ""
}

func FormatValue(name string, v *float64, withLabel bool) string {
	if v == nil || math.IsNaN(*v) {
		return notApplic
	}
	o, ok := Lookup(name)
	if !ok {
		return fmt.Sprintf("%g", *v)
	}
	s := fmt.Sprintf(o.Format, *v)
	if s == "-0" || s == "-0.0" || s == "-0.00" {
		s = s[1:]
	}
	if withLabel {
		return s + o.Unit
	}
	return s
}

func Ordinal(dir *float64) string {
	if dir == nil || math.IsNaN(*dir) {
		return notApplic
	}
	idx := int(math.Floor(math.Mod(*dir+11.25, 360) / 22.5))
	compass := []string{"N", "NNE", "NE", "ENE", "E", "ESE", "SE", "SSE", "S", "SSW", "SW", "WSW", "W", "WNW", "NW", "NNW"}
	return compass[idx%len(compass)]
}

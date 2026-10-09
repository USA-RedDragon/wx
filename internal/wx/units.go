package wx

import "math"

const (
	USUnits    = 1
	MetricWX   = 17
	mmPerInch  = 25.4
	mpsToMph   = 2.2369362920544
	hPaPerInHg = 33.8638866667
	feetPerM   = 3.28083989501
)

func CToF(c float64) float64      { return c*9/5 + 32 }
func FToC(f float64) float64      { return (f - 32) * 5 / 9 }
func MpsToMph(v float64) float64  { return v * mpsToMph }
func MmToIn(v float64) float64    { return v / mmPerInch }
func HPaToInHg(v float64) float64 { return v / hPaPerInHg }
func MToFeet(v float64) float64   { return v * feetPerM }
func PsiToInHg(v float64) float64 { return v * 2.03602 }

func KmToMiles(v float64) float64 { return v * 0.621371192 }

func FromMetricWX(name string, v float64) float64 {
	switch name {
	case OutTemp, InTemp, Dewpoint, HeatIndex, WindChill, Frostpoint:
		return CToF(v)
	case WindSpeed, WindGust:
		return MpsToMph(v)
	case Rain:
		return MmToIn(v)
	case Barometer, Pressure:
		return HPaToInHg(v)
	case Cloudbase:
		return MToFeet(v)
	case LightningDistance:
		return KmToMiles(v)
	}
	return v
}

func DewpointF(tempF, rh float64) float64 {
	if rh <= 0 {
		return math.NaN()
	}
	t := FToC(tempF)
	a, b, c := 6.1121, 17.368, 238.88
	if t < 0 {
		b, c = 17.966, 247.15
	}
	pv := rh / 100 * a * math.Exp(b*t/(c+t))
	l := math.Log(pv / a)
	return CToF(c * l / (b - l))
}

func HeatIndexF(t, rh float64) float64 {
	if t < 80 {
		return 0.5 * (t + 61.0 + (t-68.0)*1.2 + rh*0.094)
	}
	hi := -42.379 + 2.04901523*t + 10.14333127*rh - 0.22475541*t*rh - 0.00683783*t*t -
		0.05481717*rh*rh + 0.00122874*t*t*rh + 0.00085282*t*rh*rh - 0.00000199*t*t*rh*rh
	switch {
	case rh < 13 && t <= 112:
		hi -= (13 - rh) / 4 * math.Sqrt((17-math.Abs(t-95))/17)
	case rh > 85 && t <= 87:
		hi += (rh - 85) / 10 * ((87 - t) / 5)
	}
	return hi
}

func WindChillF(t, mph float64) (float64, bool) {
	if mph < 3 || t >= 50 {
		return 0, false
	}
	p := math.Pow(mph, 0.16)
	return 35.74 + 0.6215*t - 35.75*p + 0.4275*t*p, true
}

func FrostpointF(tF, dF float64) float64 {
	tk := FToC(tF) + 273.15
	dk := FToC(dF) + 273.15
	fk := dk - tk + 2671.02/((2954.61/tk)+2.193665*math.Log(tk)-13.3448)
	return CToF(fk - 273.15)
}

func AltimeterInHg(stationInHg, elevFt float64) float64 {
	return math.Pow(math.Pow(stationInHg, 0.1903)+1.313e-5*elevFt, 5.255)
}

func SeaLevelInHg(stationInHg, elevFt, tempF float64) float64 {
	tK := FToC(tempF) + 273.15
	return stationInHg / math.Exp(-(elevFt/feetPerM)/(tK*29.263))
}

func DerivePressure(v map[string]float64, elevFt float64) {
	r := PressureReducer{ElevFt: elevFt}
	r.Apply(0, v)
}

const temperatureCarry = 6 * 3600

type PressureReducer struct {
	ElevFt     float64
	lastTemp   float64
	lastTempAt int64
	hasTemp    bool
}

func (r *PressureReducer) Apply(ts int64, v map[string]float64) {
	t, okT := v[OutTemp]
	if okT {
		r.lastTemp, r.lastTempAt, r.hasTemp = t, ts, true
	} else if r.hasTemp && ts-r.lastTempAt <= temperatureCarry {
		t, okT = r.lastTemp, true
	}
	p, ok := v[Pressure]
	if !ok || p <= 0 {
		return
	}
	v[Altimeter] = AltimeterInHg(p, r.ElevFt)
	if okT {
		v[Barometer] = SeaLevelInHg(p, r.ElevFt, t)
	} else {
		v[Barometer] = v[Altimeter]
	}
}

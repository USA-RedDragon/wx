package almanac_test

import (
	"math"
	"testing"
	"time"

	"github.com/USA-RedDragon/wx/internal/almanac"
)

const (
	lat = 35.32472
	lng = -97.46061
)

func near(t *testing.T, name string, got time.Time, want string, loc *time.Location, tol time.Duration) {
	t.Helper()
	w, err := time.ParseInLocation("2006-01-02 15:04:05", want, loc)
	if err != nil {
		t.Fatal(err)
	}
	if d := got.Sub(w); d > tol || d < -tol {
		t.Errorf("%s = %s, want %s", name, got.In(loc), w)
	}
}

func chicago(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestMatchesWeewxReference(t *testing.T) {
	t.Parallel()
	loc := chicago(t)
	at := time.Date(2024, 11, 26, 2, 1, 30, 0, loc)
	temp := -1.7
	o := almanac.Observer{Lat: lat, Lon: lng, TempC: &temp}
	sun := o.Sun(at)
	if !sun.HasRise || !sun.HasSet {
		t.Fatal("expected sunrise and sunset")
	}
	near(t, "civil dawn", sun.CivilDawn, "2024-11-26 06:47:43", loc, 30*time.Second)
	near(t, "sunrise", sun.Rise, "2024-11-26 07:15:40", loc, 30*time.Second)
	near(t, "transit", sun.Transit, "2024-11-26 12:17:21", loc, 30*time.Second)
	near(t, "sunset", sun.Set, "2024-11-26 17:18:46", loc, 30*time.Second)
	near(t, "civil dusk", sun.CivilDusk, "2024-11-26 17:46:43", loc, 30*time.Second)
	if math.Abs(sun.Azimuth-65.0) > 0.3 || math.Abs(sun.Altitude+63.1) > 0.3 || math.Abs(sun.RightAscension-242.6) > 0.2 || math.Abs(sun.Decl+21.1) > 0.2 {
		t.Errorf("sun position az=%.1f alt=%.1f ra=%.1f dec=%.1f", sun.Azimuth, sun.Altitude, sun.RightAscension, sun.Decl)
	}
	if d := sun.Daylight; d < 10*time.Hour+2*time.Minute+30*time.Second || d > 10*time.Hour+3*time.Minute+40*time.Second {
		t.Errorf("daylight %v", d)
	}
	if c := sun.DaylightChange; c > -60*time.Second || c < -90*time.Second {
		t.Errorf("daylight change %v", c)
	}
	moon := o.Moon(at)
	near(t, "moonrise", moon.Rise, "2024-11-26 02:58:11", loc, time.Minute)
	near(t, "moon transit", moon.Transit, "2024-11-26 08:54:19", loc, time.Minute)
	near(t, "moonset", moon.Set, "2024-11-26 14:42:31", loc, time.Minute)
	if math.Abs(moon.Azimuth-87.6) > 0.5 || math.Abs(moon.Altitude+12.0) > 0.5 || math.Abs(moon.RightAscension-189.9) > 0.5 || math.Abs(moon.Decl+4.9) > 0.5 {
		t.Errorf("moon position az=%.1f alt=%.1f ra=%.1f dec=%.1f", moon.Azimuth, moon.Altitude, moon.RightAscension, moon.Decl)
	}
	if moon.PhaseName != "Waning crescent" || moon.Fullness < 19 || moon.Fullness > 23 {
		t.Errorf("phase %s %d%%", moon.PhaseName, moon.Fullness)
	}
	ev := almanac.NextEvents(at)
	near(t, "solstice", ev.NextSolstice, "2024-12-21 03:20:20", loc, 2*time.Minute)
	near(t, "equinox", ev.NextEquinox, "2025-03-20 04:01:14", loc, 2*time.Minute)
	if ev.NextNewMoon.Before(at) || ev.NextNewMoon.After(at.AddDate(0, 0, 5)) {
		t.Errorf("next new moon %v", ev.NextNewMoon)
	}
	if !ev.NextFullMoon.After(ev.NextNewMoon) {
		t.Errorf("next full moon %v", ev.NextFullMoon)
	}
}

func TestSummerMatchesWeewx(t *testing.T) {
	t.Parallel()
	loc := chicago(t)
	o := almanac.Observer{Lat: lat, Lon: lng}
	at := time.Date(2025, 7, 12, 12, 31, 54, 0, loc)
	sun := o.Sun(at)
	near(t, "sunrise", sun.Rise, "2025-07-12 06:24:29", loc, 30*time.Second)
	near(t, "sunset", sun.Set, "2025-07-12 20:46:21", loc, 30*time.Second)
	moon := o.Moon(at)
	near(t, "moonrise", moon.Rise, "2025-07-12 22:25:23", loc, time.Minute)
	near(t, "moonset", moon.Set, "2025-07-12 08:01:19", loc, time.Minute)
}

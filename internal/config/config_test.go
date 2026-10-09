package config_test

import (
	"errors"
	"testing"

	"github.com/USA-RedDragon/configulator/v2"
	"github.com/USA-RedDragon/wx/internal/config"
)

func TestValidate(t *testing.T) {
	t.Parallel()
	def, err := configulator.New(config.ConfigSchema()).Default()
	if err != nil {
		t.Fatal(err)
	}
	def.Station.Location, def.Station.Latitude, def.Station.Longitude, def.Station.AltitudeFeet = "Somewhere", 35, -97, 1000
	if err := def.Validate(); err != nil {
		t.Fatalf("default config invalid: %v", err)
	}
	tests := []struct {
		name string
		mod  func(*config.Config)
		want error
	}{
		{"log level", func(c *config.Config) { c.LogLevel = "loud" }, config.ErrInvalidLogLevel},
		{"archive seconds", func(c *config.Config) { c.Station.ArchiveSecs = 30 }, config.ErrInvalidArchive},
		{"archive not dividing hour", func(c *config.Config) { c.Station.ArchiveSecs = 420 }, config.ErrInvalidArchive},
		{"rain year", func(c *config.Config) { c.Station.RainYearStart = 13 }, config.ErrInvalidRainYearStart},
		{"week start", func(c *config.Config) { c.Station.WeekStart = 7 }, config.ErrInvalidWeekStart},
		{"latitude", func(c *config.Config) { c.Station.Latitude = 91 }, config.ErrInvalidLatitude},
		{"longitude", func(c *config.Config) { c.Station.Longitude = -181 }, config.ErrInvalidLongitude},
		{"nws user agent", func(c *config.Config) { c.NWS.Enabled = true }, config.ErrNWSUserAgent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := def
			tt.mod(&c)
			if err := c.Validate(); !errors.Is(err, tt.want) {
				t.Errorf("Validate() = %v, want %v", err, tt.want)
			}
		})
	}
	if def.Station.Timezone == nil || def.Station.Timezone.String() != "UTC" {
		t.Errorf("timezone %v", def.Station.Timezone)
	}
}

func TestStationIsRequired(t *testing.T) {
	t.Parallel()
	if _, err := configulator.New(config.ConfigSchema()).Load(); err == nil {
		t.Error("expected an error without station settings")
	}
}

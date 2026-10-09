package config

//go:generate go tool configulator -type Config

import (
	"errors"
	"time"
)

type LogLevel string

const (
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)

type Config struct {
	LogLevel LogLevel `name:"log-level" description:"Logging level for the application. One of debug, info, warn, or error" default:"info"`
	HTTP     HTTP     `name:"http" description:"HTTP server configuration"`
	MQTT     MQTT     `name:"mqtt" description:"MQTT subscriber configuration"`
	Station  Station  `name:"station" description:"Weather station description"`
	Database Database `name:"database" description:"Archive database configuration"`
	NWS      NWS      `name:"nws" description:"National Weather Service forecast and alerts"`
}

type HTTP struct {
	Bind string `name:"bind" description:"Address to listen on. The default, [::], listens on all interfaces" default:"[::]"`
	Port int    `name:"port" description:"Port to listen on" default:"8080"`
}

type MQTT struct {
	Broker    string `name:"broker" description:"MQTT broker URL" default:"mqtt://localhost:1883"`
	Username  string `name:"username" description:"MQTT username"`
	Password  string `name:"password" description:"MQTT password" secret:"true"`
	ClientID  string `name:"client-id" description:"MQTT client ID" default:"wx"`
	Topic     string `name:"topic" description:"Topic carrying mqtt-wx station packets in weewx METRICWX units" default:"weather/station"`
	RainTopic string `name:"rain-topic" description:"Optional rtl_433 topic whose rain_mm counter is used for rain instead of the station packet"`
}

type Station struct {
	Location      string         `name:"location" description:"Station name shown in the title bar" required:"true"`
	Latitude      float64        `name:"latitude" description:"Station latitude in degrees, north positive" required:"true"`
	Longitude     float64        `name:"longitude" description:"Station longitude in degrees, east positive" required:"true"`
	AltitudeFeet  float64        `name:"altitude-feet" description:"Altitude of the pressure sensor in feet, used to reduce station pressure to sea level" required:"true"`
	Timezone      *time.Location `name:"timezone" description:"Timezone used for day boundaries and display" default:"UTC"`
	RainYearStart int            `name:"rain-year-start" description:"Month the rain year starts in" default:"1"`
	WeekStart     int            `name:"week-start" description:"Day the week starts on, 0 is Monday and 6 is Sunday" default:"6"`
	ArchiveSecs   int            `name:"archive-interval" description:"Archive record interval in seconds" default:"60"`
	RadarImage    string         `name:"radar-image" description:"Radar image URL"`
	RadarURL      string         `name:"radar-url" description:"Radar link URL"`
	SatelliteImg  string         `name:"satellite-image" description:"Satellite image URL"`
	SatelliteURL  string         `name:"satellite-url" description:"Satellite link URL"`
}

type Database struct {
	Path string `name:"path" description:"Path to the SQLite archive database" default:"wx.db"`
}

type NWS struct {
	Enabled   bool          `name:"enabled" description:"Fetch NWS forecasts and alerts" default:"false"`
	UserAgent string        `name:"user-agent" description:"User-Agent sent to api.weather.gov, which asks for a contact such as (site, email)"`
	Interval  time.Duration `name:"interval" description:"How often to refresh the forecast" default:"30m"`
}

var (
	ErrInvalidLogLevel      = errors.New("invalid log level provided")
	ErrInvalidArchive       = errors.New("archive interval must be a whole number of minutes that divides an hour")
	ErrInvalidRainYearStart = errors.New("rain year start must be a month between 1 and 12")
	ErrInvalidWeekStart     = errors.New("week start must be between 0 and 6")
	ErrInvalidLatitude      = errors.New("latitude must be between -90 and 90")
	ErrInvalidLongitude     = errors.New("longitude must be between -180 and 180")
	ErrNWSUserAgent         = errors.New("nws.user-agent is required when nws.enabled is true")
)

func (c Config) Validate() error {
	if c.LogLevel != LogLevelDebug &&
		c.LogLevel != LogLevelInfo &&
		c.LogLevel != LogLevelWarn &&
		c.LogLevel != LogLevelError {
		return ErrInvalidLogLevel
	}
	if c.Station.ArchiveSecs < 60 || c.Station.ArchiveSecs > 3600 || 3600%c.Station.ArchiveSecs != 0 || c.Station.ArchiveSecs%60 != 0 {
		return ErrInvalidArchive
	}
	if c.Station.RainYearStart < 1 || c.Station.RainYearStart > 12 {
		return ErrInvalidRainYearStart
	}
	if c.Station.WeekStart < 0 || c.Station.WeekStart > 6 {
		return ErrInvalidWeekStart
	}
	if c.Station.Latitude < -90 || c.Station.Latitude > 90 {
		return ErrInvalidLatitude
	}
	if c.Station.Longitude < -180 || c.Station.Longitude > 180 {
		return ErrInvalidLongitude
	}
	if c.NWS.Enabled && c.NWS.UserAgent == "" {
		return ErrNWSUserAgent
	}
	return nil
}

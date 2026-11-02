// Package config loads and checks the poller settings.
//
// Settings that are safe to read aloud come from an optional YAML file.
// The secret key, the feed URLs, and the InfluxDB URL and token come only from
// the environment. No error from this package contains any of them.
package config

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/evandhoffman/trmnl-pws/internal/countdown"
	"github.com/evandhoffman/trmnl-pws/internal/holidays"
	"github.com/evandhoffman/trmnl-pws/internal/influx"
	"github.com/evandhoffman/trmnl-pws/internal/solar"
)

const (
	// MinKeyLength is the fewest characters the secret key may have.
	MinKeyLength = 32

	envKey       = "TRMNL_POLLER_KEY"
	envTripURL   = "TRIPIT_ICAL_URL"
	envSchoolURL = "SCHOOL_ICAL_URL"
	envInfluxURL = "INFLUX_URL"
	envInfluxTok = "INFLUX_TOKEN"
)

// Config is the checked configuration. TripURL and SchoolURL may be empty:
// an empty URL means the source is not configured. The same holds for
// InfluxURL: with none, the solar paths are off. Latitude and Longitude are
// both set or both nil.
type Config struct {
	Listen      string
	Timezone    string
	SchoolLabel string
	SchoolRules countdown.SchoolRules
	HolidayKeys []string

	InfluxOrg        string
	InfluxBucket     string
	Entities         solar.Entities
	NetZeroKW        float64
	TodayWindowHours int // Solar Today chart length, 1 to 24
	Latitude         *float64
	Longitude        *float64

	Key         string
	TripURL     string
	SchoolURL   string
	InfluxURL   string
	InfluxToken string
}

// fileConfig is the shape of the YAML file. A key that is absent keeps its
// default. A key that is present replaces the default, even with an empty list.
type fileConfig struct {
	Listen      string   `yaml:"listen"`
	Timezone    string   `yaml:"timezone"`
	SchoolLabel string   `yaml:"school_label"`
	Holidays    []string `yaml:"holidays"`
	SchoolRules struct {
		Include []string `yaml:"include"`
		Exclude []string `yaml:"exclude"`
	} `yaml:"school_rules"`
	Influx struct {
		Org    string `yaml:"org"`
		Bucket string `yaml:"bucket"`
	} `yaml:"influx"`
	Solar struct {
		Entities         entitiesFile `yaml:"entities"`
		NetZeroKW        float64      `yaml:"net_zero_kw"`
		TodayWindowHours int          `yaml:"today_window_hours"`
	} `yaml:"solar"`
	Latitude  *float64 `yaml:"latitude"`
	Longitude *float64 `yaml:"longitude"`
}

// entitiesFile mirrors solar.Entities with YAML keys. It starts from the
// defaults, so a key that is absent keeps its default.
type entitiesFile struct {
	SolarKW    string `yaml:"solar_kw"`
	GridKW     string `yaml:"grid_kw"`
	LoadKW     string `yaml:"load_kw"`
	CarKW      string `yaml:"car_kw"`
	ChargePct  string `yaml:"charge_pct"`
	ReservePct string `yaml:"reserve_pct"`
	SolarKWh   string `yaml:"solar_kwh"`
	HomeKWh    string `yaml:"home_kwh"`
	GridInKWh  string `yaml:"grid_in_kwh"`
	GridOutKWh string `yaml:"grid_out_kwh"`
}

func defaults() fileConfig {
	f := fileConfig{
		Listen:      ":8790",
		Timezone:    "America/New_York",
		SchoolLabel: "School",
		Holidays:    slices.Clone(holidays.DefaultKeys),
	}
	f.SchoolRules.Include = slices.Clone(countdown.DefaultSchoolRules.Include)
	f.SchoolRules.Exclude = slices.Clone(countdown.DefaultSchoolRules.Exclude)
	f.Influx.Org = "bellmore"
	f.Influx.Bucket = "home_assistant/autogen"
	f.Solar.Entities = entitiesFile(solar.DefaultEntities)
	f.Solar.NetZeroKW = 0.05
	f.Solar.TodayWindowHours = 12
	return f
}

// Load reads the optional YAML file at path, then the environment, and checks
// the result. A file that does not exist is not an error: the defaults apply.
func Load(path string, getenv func(string) string) (Config, error) {
	f := defaults()
	if err := readFile(path, &f); err != nil {
		return Config{}, err
	}
	cfg := Config{
		Listen:           f.Listen,
		Timezone:         f.Timezone,
		SchoolLabel:      f.SchoolLabel,
		SchoolRules:      countdown.SchoolRules{Include: f.SchoolRules.Include, Exclude: f.SchoolRules.Exclude},
		HolidayKeys:      f.Holidays,
		InfluxOrg:        f.Influx.Org,
		InfluxBucket:     f.Influx.Bucket,
		Entities:         solar.Entities(f.Solar.Entities),
		NetZeroKW:        f.Solar.NetZeroKW,
		TodayWindowHours: f.Solar.TodayWindowHours,
		Latitude:         f.Latitude,
		Longitude:        f.Longitude,
		Key:              getenv(envKey),
		TripURL:          getenv(envTripURL),
		SchoolURL:        getenv(envSchoolURL),
		InfluxURL:        getenv(envInfluxURL),
		InfluxToken:      getenv(envInfluxTok),
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func readFile(path string, f *fileConfig) error {
	file, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("config file: %w", err)
	}
	defer func() { _ = file.Close() }()
	dec := yaml.NewDecoder(file)
	dec.KnownFields(true)
	if err := dec.Decode(f); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("config file %s: %w", path, err)
	}
	return nil
}

func (c Config) validate() error {
	switch {
	case c.Key == "":
		return fmt.Errorf("%s is not set", envKey)
	case len(c.Key) < MinKeyLength:
		return fmt.Errorf("%s must have at least %d characters", envKey, MinKeyLength)
	case !KeyCharsOK(c.Key):
		return fmt.Errorf("%s may only contain letters, digits, '-' and '_'", envKey)
	}
	for _, f := range []struct{ env, value string }{
		{envTripURL, c.TripURL},
		{envSchoolURL, c.SchoolURL},
	} {
		if err := checkFeedURL(f.env, f.value); err != nil {
			return err
		}
	}
	if c.Listen == "" {
		return errors.New("listen must not be empty")
	}
	if c.SchoolLabel == "" {
		return errors.New("school_label must not be empty")
	}
	if c.Timezone == "" || c.Timezone == "Local" {
		return errors.New("timezone must name an IANA zone such as America/New_York")
	}
	if _, err := time.LoadLocation(c.Timezone); err != nil {
		return fmt.Errorf("timezone %q: %w", c.Timezone, err)
	}
	if len(c.HolidayKeys) == 0 {
		return errors.New("holidays: list at least one holiday, or remove the key to use the defaults")
	}
	if err := holidays.Validate(c.HolidayKeys); err != nil {
		return fmt.Errorf("holidays: %w", err)
	}
	if err := c.SchoolRules.Validate(); err != nil {
		return fmt.Errorf("school_rules: %w", err)
	}
	return c.validateSolar()
}

// validateSolar checks the InfluxDB and solar settings. Its errors are fixed
// text, so none holds the InfluxDB URL or token.
func (c Config) validateSolar() error {
	if c.InfluxURL != "" {
		if err := checkInfluxURL(c.InfluxURL); err != nil {
			return err
		}
		if c.InfluxToken == "" {
			return fmt.Errorf("%s is not set, and %s needs it", envInfluxTok, envInfluxURL)
		}
	}
	if c.InfluxOrg == "" {
		return errors.New("influx.org must not be empty")
	}
	if c.InfluxBucket == "" {
		return errors.New("influx.bucket must not be empty")
	}
	for _, e := range []struct{ key, id string }{
		{"solar_kw", c.Entities.SolarKW},
		{"grid_kw", c.Entities.GridKW},
		{"load_kw", c.Entities.LoadKW},
		{"car_kw", c.Entities.CarKW},
		{"charge_pct", c.Entities.ChargePct},
		{"reserve_pct", c.Entities.ReservePct},
		{"solar_kwh", c.Entities.SolarKWh},
		{"home_kwh", c.Entities.HomeKWh},
		{"grid_in_kwh", c.Entities.GridInKWh},
		{"grid_out_kwh", c.Entities.GridOutKWh},
	} {
		if !influx.ValidEntity(e.id) {
			return fmt.Errorf("solar.entities.%s must use only a-z, 0-9 and '_'", e.key)
		}
	}
	// NaN fails this test too.
	if !(c.NetZeroKW > 0) {
		return errors.New("solar.net_zero_kw must be greater than 0")
	}
	if c.TodayWindowHours < 1 || c.TodayWindowHours > 24 {
		return errors.New("solar.today_window_hours must be from 1 to 24")
	}
	if (c.Latitude == nil) != (c.Longitude == nil) {
		return errors.New("latitude and longitude: set both or neither")
	}
	if c.Latitude != nil {
		if !(*c.Latitude >= -90 && *c.Latitude <= 90) {
			return errors.New("latitude must be between -90 and 90")
		}
		if !(*c.Longitude >= -180 && *c.Longitude <= 180) {
			return errors.New("longitude must be between -180 and 180")
		}
	}
	return nil
}

// KeyCharsOK reports whether key has only letters, digits, '-' and '_'.
//
// TRMNL also parses a "name=value&name=value" form of the polling header. A
// key with '&', '=', ':' or '#' could be split there, and every poll would
// return 404. The set is plain ASCII, so no header form can change it.
func KeyCharsOK(key string) bool {
	if key == "" {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

// checkFeedURL refuses a set feed URL that is not an https URL with a host.
// The error is fixed text. It never holds the value or the parse error,
// because the TripIt URL carries a token.
func checkFeedURL(envName, value string) error {
	if value == "" {
		return nil
	}
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must be an https URL", envName)
	}
	return nil
}

// checkInfluxURL refuses a URL that is not http or https with a host. The
// error is fixed text: the value may name an internal host.
func checkInfluxURL(value string) error {
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must be an http or https URL", envInfluxURL)
	}
	return nil
}

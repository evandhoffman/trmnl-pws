// Package config loads and checks the poller settings.
//
// Settings that are safe to read aloud come from an optional YAML file.
// The secret key and the feed URLs come only from the environment. No error
// from this package contains the key or a feed URL.
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
)

const (
	// MinKeyLength is the fewest characters the secret key may have.
	MinKeyLength = 32

	envKey       = "TRMNL_POLLER_KEY"
	envTripURL   = "TRIPIT_ICAL_URL"
	envSchoolURL = "SCHOOL_ICAL_URL"
)

// Config is the checked configuration. TripURL and SchoolURL may be empty:
// an empty URL means the source is not configured.
type Config struct {
	Listen      string
	Timezone    string
	SchoolLabel string
	SchoolRules countdown.SchoolRules
	HolidayKeys []string

	Key       string
	TripURL   string
	SchoolURL string
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
		Listen:      f.Listen,
		Timezone:    f.Timezone,
		SchoolLabel: f.SchoolLabel,
		SchoolRules: countdown.SchoolRules{Include: f.SchoolRules.Include, Exclude: f.SchoolRules.Exclude},
		HolidayKeys: f.Holidays,
		Key:         getenv(envKey),
		TripURL:     getenv(envTripURL),
		SchoolURL:   getenv(envSchoolURL),
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

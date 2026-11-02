package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/evandhoffman/trmnl-pws/internal/countdown"
	"github.com/evandhoffman/trmnl-pws/internal/holidays"
)

// goodKey is 32 characters, the shortest key Load accepts.
const goodKey = "0123456789abcdef0123456789abcdef"

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func writeFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func missingPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "absent.yml")
}

func TestLoadDefaultsWithoutFile(t *testing.T) {
	cfg, err := Load(missingPath(t), env(map[string]string{
		"TRMNL_POLLER_KEY": goodKey,
		"TRIPIT_ICAL_URL":  "https://example.invalid/trips.ics",
		"SCHOOL_ICAL_URL":  "https://example.invalid/school.ics",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != ":8790" || cfg.Timezone != "America/New_York" || cfg.SchoolLabel != "School" {
		t.Fatalf("scalar defaults wrong: %+v", cfg)
	}
	if !reflect.DeepEqual(cfg.SchoolRules, countdown.DefaultSchoolRules) {
		t.Fatalf("school rules: %+v", cfg.SchoolRules)
	}
	if !reflect.DeepEqual(cfg.HolidayKeys, holidays.DefaultKeys) {
		t.Fatalf("holiday keys: %v", cfg.HolidayKeys)
	}
	if cfg.Key != goodKey || cfg.TripURL != "https://example.invalid/trips.ics" ||
		cfg.SchoolURL != "https://example.invalid/school.ics" {
		t.Fatalf("env values wrong: %+v", cfg)
	}
}

func TestDefaultsDoNotAliasPackageState(t *testing.T) {
	cfg, err := Load(missingPath(t), env(map[string]string{"TRMNL_POLLER_KEY": goodKey}))
	if err != nil {
		t.Fatal(err)
	}
	cfg.HolidayKeys[0] = "mutated"
	cfg.SchoolRules.Include[0] = "mutated"
	cfg.SchoolRules.Exclude[0] = "mutated"
	if holidays.DefaultKeys[0] == "mutated" {
		t.Fatal("HolidayKeys aliases holidays.DefaultKeys")
	}
	if countdown.DefaultSchoolRules.Include[0] == "mutated" ||
		countdown.DefaultSchoolRules.Exclude[0] == "mutated" {
		t.Fatal("SchoolRules alias countdown.DefaultSchoolRules")
	}
}

func TestFileOverridesDefaults(t *testing.T) {
	p := writeFile(t, `
listen: "127.0.0.1:9000"
timezone: America/Chicago
school_label: Hogwarts
school_rules:
  include: ['^Feast']
  exclude: ['^Quidditch']
holidays: [thanksgiving, christmas]
`)
	cfg, err := Load(p, env(map[string]string{"TRMNL_POLLER_KEY": goodKey}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "127.0.0.1:9000" || cfg.Timezone != "America/Chicago" || cfg.SchoolLabel != "Hogwarts" {
		t.Fatalf("scalars: %+v", cfg)
	}
	want := countdown.SchoolRules{Include: []string{"^Feast"}, Exclude: []string{"^Quidditch"}}
	if !reflect.DeepEqual(cfg.SchoolRules, want) {
		t.Fatalf("rules: %+v", cfg.SchoolRules)
	}
	if !reflect.DeepEqual(cfg.HolidayKeys, []string{"thanksgiving", "christmas"}) {
		t.Fatalf("holidays: %v", cfg.HolidayKeys)
	}
}

func TestPartialFileKeepsOtherDefaults(t *testing.T) {
	p := writeFile(t, "school_label: Hogwarts\n")
	cfg, err := Load(p, env(map[string]string{"TRMNL_POLLER_KEY": goodKey}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != ":8790" || !reflect.DeepEqual(cfg.SchoolRules, countdown.DefaultSchoolRules) {
		t.Fatalf("defaults lost: %+v", cfg)
	}
}

func TestRefusesMissingKey(t *testing.T) {
	_, err := Load(missingPath(t), env(nil))
	if err == nil || !strings.Contains(err.Error(), "TRMNL_POLLER_KEY") {
		t.Fatalf("got %v", err)
	}
}

func TestRefusesShortKey(t *testing.T) {
	short := strings.Repeat("k", 31)
	_, err := Load(missingPath(t), env(map[string]string{"TRMNL_POLLER_KEY": short}))
	if err == nil || !strings.Contains(err.Error(), "TRMNL_POLLER_KEY") {
		t.Fatalf("got %v", err)
	}
}

func TestErrorsNeverContainSecrets(t *testing.T) {
	const feedURL = "https://example.invalid/secret-token-path.ics"
	short := "short-secret-key-value"
	cases := map[string]string{
		"short key":     "",
		"bad holiday":   "holidays: [festivus]\n",
		"bad regex":     "school_rules:\n  include: ['(']\n",
		"bad timezone":  "timezone: Mars/Olympus\n",
		"bad yaml":      "listen: [\n",
		"unknown field": "bogus: 1\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			key := goodKey
			if name == "short key" {
				key = short
			}
			p := missingPath(t)
			if body != "" {
				p = writeFile(t, body)
			}
			_, err := Load(p, env(map[string]string{
				"TRMNL_POLLER_KEY": key,
				"TRIPIT_ICAL_URL":  feedURL,
				"SCHOOL_ICAL_URL":  feedURL,
			}))
			if err == nil {
				t.Fatal("want an error")
			}
			for _, secret := range []string{key, feedURL, "secret-token-path"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("error leaks %q: %v", secret, err)
				}
			}
		})
	}
}

func TestRefusesUnknownHoliday(t *testing.T) {
	p := writeFile(t, "holidays: [festivus]\n")
	_, err := Load(p, env(map[string]string{"TRMNL_POLLER_KEY": goodKey}))
	if err == nil || !strings.Contains(err.Error(), "holidays") {
		t.Fatalf("got %v", err)
	}
}

func TestRefusesBadRegex(t *testing.T) {
	for _, body := range []string{
		"school_rules:\n  include: ['(']\n",
		"school_rules:\n  exclude: ['[']\n",
	} {
		p := writeFile(t, body)
		_, err := Load(p, env(map[string]string{"TRMNL_POLLER_KEY": goodKey}))
		if err == nil || !strings.Contains(err.Error(), "school_rules") {
			t.Fatalf("%q: got %v", body, err)
		}
	}
}

func TestRefusesBadTimezone(t *testing.T) {
	p := writeFile(t, "timezone: Mars/Olympus\n")
	_, err := Load(p, env(map[string]string{"TRMNL_POLLER_KEY": goodKey}))
	if err == nil || !strings.Contains(err.Error(), "timezone") {
		t.Fatalf("got %v", err)
	}
}

func TestRefusesEmptyListenAndLabel(t *testing.T) {
	for _, body := range []string{"listen: ''\n", "school_label: ''\n", "timezone: ''\n", "timezone: Local\n"} {
		p := writeFile(t, body)
		_, err := Load(p, env(map[string]string{"TRMNL_POLLER_KEY": goodKey}))
		if err == nil {
			t.Fatalf("%q: want an error", body)
		}
	}
}

func TestRefusesUnknownFileKey(t *testing.T) {
	p := writeFile(t, "lisen: ':1'\n")
	_, err := Load(p, env(map[string]string{"TRMNL_POLLER_KEY": goodKey}))
	if err == nil {
		t.Fatal("a misspelled key must fail, not fall back to a default")
	}
}

func TestUnreadableFileFails(t *testing.T) {
	// A directory exists but cannot be read as a file: not the same as missing.
	_, err := Load(t.TempDir(), env(map[string]string{"TRMNL_POLLER_KEY": goodKey}))
	if err == nil {
		t.Fatal("want an error")
	}
}

func TestEmptyFeedURLDisablesSource(t *testing.T) {
	cfg, err := Load(missingPath(t), env(map[string]string{
		"TRMNL_POLLER_KEY": goodKey,
		"SCHOOL_ICAL_URL":  "https://example.invalid/school.ics",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TripURL != "" {
		t.Fatalf("TripURL = %q", cfg.TripURL)
	}
	if cfg.SchoolURL == "" {
		t.Fatal("SchoolURL lost")
	}
}

func TestExampleFileMatchesDefaults(t *testing.T) {
	env := env(map[string]string{"TRMNL_POLLER_KEY": goodKey})
	want, err := Load(missingPath(t), env)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Load("../../config/poller.example.yml", env)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("example file differs from the defaults:\n got %+v\nwant %+v", got, want)
	}
}

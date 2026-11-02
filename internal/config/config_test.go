package config

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/evandhoffman/trmnl-pws/internal/countdown"
	"github.com/evandhoffman/trmnl-pws/internal/holidays"
	"github.com/evandhoffman/trmnl-pws/internal/solar"
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
		"short key":      "",
		"bad holiday":    "holidays: [festivus]\n",
		"bad regex":      "school_rules:\n  include: ['(']\n",
		"bad timezone":   "timezone: Mars/Olympus\n",
		"bad yaml":       "listen: [\n",
		"unknown field":  "bogus: 1\n",
		"empty holidays": "holidays: []\n",
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

// TestBadFeedURLsAreRefusedWithoutLeaking covers each URL variable. The error
// is fixed text: it holds neither the value nor the parse error, because the
// TripIt URL carries a token.
func TestBadFeedURLsAreRefusedWithoutLeaking(t *testing.T) {
	bad := map[string]string{
		"webcal":            "webcal://example.invalid/secret-token-path.ics",
		"http":              "http://example.invalid/secret-token-path.ics",
		"no host":           "https:///secret-token-path",
		"trailing newline":  "https://example.invalid/secret-token-path.ics\n",
		"leading space":     " https://example.invalid/secret-token-path.ics",
		"bad escape":        "https://example.invalid/%zz-secret-token-path",
		"control character": "https://example.invalid/secret-token-path\x7f.ics",
		"no scheme":         "example.invalid/secret-token-path.ics",
		"port only":         "https://:443/secret-token-path.ics",
	}
	for _, name := range []string{"TRIPIT_ICAL_URL", "SCHOOL_ICAL_URL"} {
		for label, value := range bad {
			t.Run(name+"/"+label, func(t *testing.T) {
				_, err := Load(missingPath(t), env(map[string]string{
					"TRMNL_POLLER_KEY": goodKey,
					name:               value,
				}))
				if err == nil {
					t.Fatal("want an error")
				}
				want := name + " must be an https URL"
				if err.Error() != want {
					t.Fatalf("error = %q, want %q", err, want)
				}
				if strings.Contains(err.Error(), "secret-token-path") || strings.Contains(err.Error(), "example.invalid") {
					t.Fatalf("error leaks the value: %v", err)
				}
				if errors.Unwrap(err) != nil {
					t.Fatal("error wraps another error")
				}
			})
		}
	}
}

func TestRefusesKeyOutsideAllowedSet(t *testing.T) {
	const pad = "0123456789abcdef0123456789abcde" // 31 characters
	for label, ch := range map[string]string{
		"ampersand": "&", "equals": "=", "colon": ":", "hash": "#", "space": " ",
		"non-ASCII letter": "\u00e9", "semicolon": ";", "newline": "\n", "quote": `"`,
	} {
		t.Run(label, func(t *testing.T) {
			key := pad + ch + "xy"
			_, err := Load(missingPath(t), env(map[string]string{"TRMNL_POLLER_KEY": key}))
			if err == nil {
				t.Fatal("want an error")
			}
			if !strings.Contains(err.Error(), "TRMNL_POLLER_KEY") ||
				!strings.Contains(err.Error(), "letters, digits") {
				t.Errorf("error does not name the variable and the rule: %v", err)
			}
			if strings.Contains(err.Error(), key) || strings.Contains(err.Error(), pad) {
				t.Errorf("error leaks the key: %v", err)
			}
		})
	}
}

func TestAcceptsKeyWithDashAndUnderscore(t *testing.T) {
	key := "abcdEFGH-1234_ijkl-5678_MNOP-9012_qr"
	if _, err := Load(missingPath(t), env(map[string]string{"TRMNL_POLLER_KEY": key})); err != nil {
		t.Fatal(err)
	}
}

func TestRefusesEmptyHolidayList(t *testing.T) {
	p := writeFile(t, "holidays: []\n")
	_, err := Load(p, env(map[string]string{"TRMNL_POLLER_KEY": goodKey}))
	if err == nil || !strings.Contains(err.Error(), "holidays") || !strings.Contains(err.Error(), "at least one") {
		t.Fatalf("got %v", err)
	}
}

func TestAbsentHolidaysKeepsDefaults(t *testing.T) {
	p := writeFile(t, "school_label: Hogwarts\n")
	cfg, err := Load(p, env(map[string]string{"TRMNL_POLLER_KEY": goodKey}))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.HolidayKeys, holidays.DefaultKeys) {
		t.Fatalf("holidays: %v", cfg.HolidayKeys)
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

func TestSolarDefaults(t *testing.T) {
	cfg, err := Load(missingPath(t), env(map[string]string{"TRMNL_POLLER_KEY": goodKey}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.InfluxOrg != "bellmore" || cfg.InfluxBucket != "home_assistant/autogen" {
		t.Fatalf("influx defaults: %q %q", cfg.InfluxOrg, cfg.InfluxBucket)
	}
	if cfg.Entities != solar.DefaultEntities {
		t.Fatalf("entities: %+v", cfg.Entities)
	}
	if cfg.NetZeroKW != 0.05 {
		t.Fatalf("net_zero_kw = %v", cfg.NetZeroKW)
	}
	if cfg.Latitude != nil || cfg.Longitude != nil {
		t.Fatal("coordinates must default to unset")
	}
	if cfg.InfluxURL != "" || cfg.InfluxToken != "" {
		t.Fatalf("influx env: %q %q", cfg.InfluxURL, cfg.InfluxToken)
	}
}

func TestInfluxEnvIsRead(t *testing.T) {
	cfg, err := Load(missingPath(t), env(map[string]string{
		"TRMNL_POLLER_KEY": goodKey,
		"INFLUX_URL":       "http://influxdb2.invalid:8086",
		"INFLUX_TOKEN":     "tok-123",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.InfluxURL != "http://influxdb2.invalid:8086" || cfg.InfluxToken != "tok-123" {
		t.Fatalf("influx env: %q %q", cfg.InfluxURL, cfg.InfluxToken)
	}
}

func TestSolarFileOverrides(t *testing.T) {
	p := writeFile(t, `
influx:
  org: other
  bucket: ha/week
solar:
  net_zero_kw: 0.2
  entities:
    grid_kw: my_grid_power
    car_kw: my_car_power
latitude: 41.5
longitude: -73.25
`)
	cfg, err := Load(p, env(map[string]string{"TRMNL_POLLER_KEY": goodKey}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.InfluxOrg != "other" || cfg.InfluxBucket != "ha/week" || cfg.NetZeroKW != 0.2 {
		t.Fatalf("scalars: %+v", cfg)
	}
	want := solar.DefaultEntities
	want.GridKW, want.CarKW = "my_grid_power", "my_car_power"
	if cfg.Entities != want {
		t.Fatalf("entities: %+v", cfg.Entities)
	}
	if cfg.Latitude == nil || *cfg.Latitude != 41.5 || cfg.Longitude == nil || *cfg.Longitude != -73.25 {
		t.Fatalf("coordinates: %v %v", cfg.Latitude, cfg.Longitude)
	}
}

func TestSolarRefusals(t *testing.T) {
	cases := map[string]struct {
		body string
		env  map[string]string
	}{
		"entity with space":     {body: "solar:\n  entities:\n    grid_kw: 'a b'\n"},
		"entity with quote":     {body: "solar:\n  entities:\n    solar_kw: 'x\"y'\n"},
		"entity uppercase":      {body: "solar:\n  entities:\n    load_kw: Load\n"},
		"entity empty":          {body: "solar:\n  entities:\n    car_kw: ''\n"},
		"entity with dot":       {body: "solar:\n  entities:\n    charge_pct: sensor.charge\n"},
		"unknown entity key":    {body: "solar:\n  entities:\n    bogus_kw: x\n"},
		"net zero zero":         {body: "solar:\n  net_zero_kw: 0\n"},
		"net zero negative":     {body: "solar:\n  net_zero_kw: -1\n"},
		"empty org":             {body: "influx:\n  org: ''\n"},
		"empty bucket":          {body: "influx:\n  bucket: ''\n"},
		"latitude only":         {body: "latitude: 41.5\n"},
		"longitude only":        {body: "longitude: -73.2\n"},
		"latitude out of range": {body: "latitude: 91\nlongitude: 0\n"},
		"longitude too big":     {body: "latitude: 0\nlongitude: 181\n"},
		"url without token":     {env: map[string]string{"INFLUX_URL": "http://influxdb2.invalid:8086"}},
		"url not http":          {env: map[string]string{"INFLUX_URL": "ftp://influxdb2.invalid", "INFLUX_TOKEN": "t"}},
		"url no host":           {env: map[string]string{"INFLUX_URL": "http://", "INFLUX_TOKEN": "t"}},
		"url garbage":           {env: map[string]string{"INFLUX_URL": "::not a url::", "INFLUX_TOKEN": "t"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			p := missingPath(t)
			if tc.body != "" {
				p = writeFile(t, tc.body)
			}
			e := map[string]string{"TRMNL_POLLER_KEY": goodKey}
			maps.Copy(e, tc.env)
			if _, err := Load(p, env(e)); err == nil {
				t.Fatal("want an error")
			}
		})
	}
}

func TestSolarErrorsNeverContainInfluxSecrets(t *testing.T) {
	const token = "s3cret-influx-token-value"
	const baseURL = "http://secret-host.invalid:8086/path"
	cases := map[string]struct {
		body string
		env  map[string]string
	}{
		"bad url":           {env: map[string]string{"INFLUX_URL": "ftp://secret-host.invalid:8086/path"}},
		"url without token": {env: map[string]string{"INFLUX_URL": baseURL}},
		"bad entity":        {body: "solar:\n  entities:\n    grid_kw: 'A B'\n", env: map[string]string{"INFLUX_URL": baseURL}},
		"bad net zero":      {body: "solar:\n  net_zero_kw: 0\n", env: map[string]string{"INFLUX_URL": baseURL}},
		"half coordinates":  {body: "latitude: 41\n", env: map[string]string{"INFLUX_URL": baseURL}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			p := missingPath(t)
			if tc.body != "" {
				p = writeFile(t, tc.body)
			}
			e := map[string]string{"TRMNL_POLLER_KEY": goodKey, "INFLUX_TOKEN": token}
			maps.Copy(e, tc.env)
			if name == "url without token" {
				delete(e, "INFLUX_TOKEN")
			}
			_, err := Load(p, env(e))
			if err == nil {
				t.Fatal("want an error")
			}
			for _, secret := range []string{token, "secret-host", baseURL} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("error leaks %q: %v", secret, err)
				}
			}
		})
	}
}

func TestTokenWithoutURLIsAccepted(t *testing.T) {
	cfg, err := Load(missingPath(t), env(map[string]string{"TRMNL_POLLER_KEY": goodKey, "INFLUX_TOKEN": "t"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.InfluxURL != "" {
		t.Fatal("URL must stay empty")
	}
}

func TestEntityDefaultsDoNotAliasPackageState(t *testing.T) {
	cfg, err := Load(missingPath(t), env(map[string]string{"TRMNL_POLLER_KEY": goodKey}))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Entities.GridKW = "mutated"
	if solar.DefaultEntities.GridKW == "mutated" {
		t.Fatal("Entities alias solar.DefaultEntities")
	}
}

package solar

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDayJSONNulls(t *testing.T) {
	label := "Mon 10/26"
	b, err := json.Marshal(Day{Label: &label})
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, key := range []string{
		"solar_kwh", "home_kwh", "grid_in_kwh", "grid_out_kwh",
		"self_powered_pct", "net_zero_h", "peak_pct", "full_time",
		"drain_time", "reserve_time", "car_kwh",
	} {
		if !strings.Contains(got, `"`+key+`":null`) {
			t.Errorf("%s missing or not null in %s", key, got)
		}
	}
	if !strings.Contains(got, `"label":"Mon 10/26"`) {
		t.Errorf("label missing in %s", got)
	}
}

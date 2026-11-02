package solar

import (
	"regexp"
	"testing"
)

func TestDefaultEntities(t *testing.T) {
	all := DefaultEntities.All()
	if len(all) != 10 {
		t.Fatalf("len(All) = %d, want 10", len(all))
	}
	valid := regexp.MustCompile(`^[a-z0-9_]+$`)
	seen := map[string]bool{}
	for _, id := range all {
		if !valid.MatchString(id) {
			t.Errorf("entity %q is not a valid ID", id)
		}
		if seen[id] {
			t.Errorf("entity %q appears twice", id)
		}
		seen[id] = true
	}
	if DefaultEntities.SolarKW != "bellmore_solar_power" ||
		DefaultEntities.CarKW != "wall_connector_power" ||
		DefaultEntities.GridOutKWh != "bellmore_grid_exported" {
		t.Fatalf("unexpected defaults: %+v", DefaultEntities)
	}
}

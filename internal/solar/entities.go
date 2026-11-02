// Package solar turns Home Assistant readings into the Solar Today and Solar
// Week payloads. It reads points, runs the step-hold math, and formats every
// display value as a string.
package solar

// Entities names the Home Assistant entity for each reading.
type Entities struct {
	SolarKW    string
	GridKW     string
	LoadKW     string
	CarKW      string
	ChargePct  string
	ReservePct string
	SolarKWh   string
	HomeKWh    string
	GridInKWh  string
	GridOutKWh string
}

// DefaultEntities holds the entity IDs of the house.
var DefaultEntities = Entities{
	SolarKW:    "bellmore_solar_power",
	GridKW:     "bellmore_grid_power",
	LoadKW:     "bellmore_load_power",
	CarKW:      "wall_connector_power",
	ChargePct:  "bellmore_percentage_charged",
	ReservePct: "bellmore_backup_reserve",
	SolarKWh:   "bellmore_solar_generated",
	HomeKWh:    "bellmore_home_usage",
	GridInKWh:  "bellmore_grid_imported",
	GridOutKWh: "bellmore_grid_exported",
}

// All lists every entity ID, in field order.
func (e Entities) All() []string {
	return []string{
		e.SolarKW, e.GridKW, e.LoadKW, e.CarKW, e.ChargePct, e.ReservePct,
		e.SolarKWh, e.HomeKWh, e.GridInKWh, e.GridOutKWh,
	}
}

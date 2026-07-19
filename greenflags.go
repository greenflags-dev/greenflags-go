// Package greenflags is the official Go SDK for GreenFlags feature flags.
//
// Snapshot + cache model: one network call (Refresh) fetches every flag of
// the token's environment; every read after that is served from memory.
// See https://greenflags.dev/docs/ for the API reference.
package greenflags

import "fmt"

// FlagType is the value type of a flag: "boolean", "string", "number" or "json".
type FlagType string

const (
	FlagTypeBoolean FlagType = "boolean"
	FlagTypeString  FlagType = "string"
	FlagTypeNumber  FlagType = "number"
	FlagTypeJSON    FlagType = "json"
)

// Coordinates is a geographic point in decimal degrees.
type Coordinates struct {
	Latitude  float64
	Longitude float64
}

// Geofence is the geographic scope of a flag value: inside the radius the
// flag keeps its value, outside it evaluates to its off value.
type Geofence struct {
	Latitude     float64 `json:"latitude"`
	Longitude    float64 `json:"longitude"`
	RadiusMeters float64 `json:"radiusMeters"`
}

// Rollout is a percentage rollout rule: Percentage% of users (bucketed
// deterministically per docs/rollout-hash-spec.md) receive the flag's value.
type Rollout struct {
	Percentage int `json:"percentage"`
}

// FlagVariant is a weighted variant of a multivariate flag.
type FlagVariant struct {
	Name   string `json:"name"`
	Weight int    `json:"weight"`
	Value  any    `json:"value"`
}

// Flag is a feature flag as served by GET /v1/flags. Value holds bool,
// string, float64, map[string]any or nil depending on Type (and geofence
// evaluation).
type Flag struct {
	Key      string        `json:"key"`
	Type     FlagType      `json:"type"`
	Value    any           `json:"value"`
	Geofence *Geofence     `json:"geofence,omitempty"`
	Rollout  *Rollout      `json:"rollout,omitempty"`
	Variants []FlagVariant `json:"variants,omitempty"`
}

// Error is returned on network or API failures. Code mirrors the API error
// codes (e.g. INVALID_TOKEN, QUOTA_EXCEEDED) plus the client-side
// NETWORK_ERROR and PARSE_ERROR. Status is the HTTP status (0 for network
// failures).
type Error struct {
	Code    string
	Message string
	Status  int
}

func (e *Error) Error() string {
	return fmt.Sprintf("greenflags: %s (%d): %s", e.Code, e.Status, e.Message)
}

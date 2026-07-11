package greenflags

import (
	"math"
	"testing"
)

func TestHaversineSamePointIsZero(t *testing.T) {
	point := Coordinates{Latitude: 19.4326, Longitude: -99.1332}
	if d := haversineMeters(point, point); math.Abs(d) > 1e-6 {
		t.Fatalf("expected 0, got %f", d)
	}
}

func TestHaversineParisLondonWithinOnePercent(t *testing.T) {
	paris := Coordinates{Latitude: 48.8566, Longitude: 2.3522}
	london := Coordinates{Latitude: 51.5074, Longitude: -0.1278}
	const expected = 343550.0
	d := haversineMeters(paris, london)
	if math.Abs(d-expected)/expected >= 0.01 {
		t.Fatalf("expected ~%f, got %f", expected, d)
	}
}

func TestHaversineAntimeridian(t *testing.T) {
	a := Coordinates{Latitude: 0, Longitude: 179.9}
	b := Coordinates{Latitude: 0, Longitude: -179.9}
	d := haversineMeters(a, b)
	if d <= 15000 || d >= 30000 {
		t.Fatalf("expected small crossing distance, got %f", d)
	}
}

func TestHaversineExactPoleIsZero(t *testing.T) {
	a := Coordinates{Latitude: 90, Longitude: 0}
	b := Coordinates{Latitude: 90, Longitude: 137}
	if d := haversineMeters(a, b); math.Abs(d) > 1e-6 {
		t.Fatalf("expected 0, got %f", d)
	}
}

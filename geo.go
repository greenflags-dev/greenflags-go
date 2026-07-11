package greenflags

import "math"

const earthRadiusMeters = 6371000

func toRadians(degrees float64) float64 {
	return degrees * math.Pi / 180
}

// haversineMeters mirrors the JS/Dart/Python SDK implementations exactly.
func haversineMeters(a, b Coordinates) float64 {
	phi1 := toRadians(a.Latitude)
	phi2 := toRadians(b.Latitude)
	deltaPhi := toRadians(b.Latitude - a.Latitude)
	deltaLambda := toRadians(b.Longitude - a.Longitude)

	h := math.Pow(math.Sin(deltaPhi/2), 2) +
		math.Cos(phi1)*math.Cos(phi2)*math.Pow(math.Sin(deltaLambda/2), 2)

	c := 2 * math.Atan2(math.Sqrt(h), math.Sqrt(1-h))

	return earthRadiusMeters * c
}

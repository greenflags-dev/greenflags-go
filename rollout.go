package greenflags

import "sort"

// Deterministic rollout bucketing. Canonical algorithm:
// docs/rollout-hash-spec.md (repo root). Must stay byte-identical to every
// other GreenFlags evaluator — conformance vectors live in
// sdks/rollout-test-vectors.json.

const (
	fnvOffsetBasis uint32 = 2166136261
	fnvPrime       uint32 = 16777619
)

func fnv1a32(input string) uint32 {
	hash := fnvOffsetBasis
	// Ranging over []byte(input) iterates UTF-8 bytes, as the spec requires.
	for _, b := range []byte(input) {
		hash ^= uint32(b)
		hash *= fnvPrime // uint32 wraps mod 2^32 natively
	}
	return hash
}

// RolloutBucket returns the 0-99 bucket a user falls into for a given flag.
// Stable for the same flagKey + userKey pair across every GreenFlags SDK and
// the server.
func RolloutBucket(flagKey, userKey string) int {
	return int(fnv1a32(flagKey+":"+userKey) % 100)
}

// IsIncludedInRollout reports whether the user is inside the rollout
// percentage for the flag.
func IsIncludedInRollout(flagKey, userKey string, percentage int) bool {
	return RolloutBucket(flagKey, userKey) < percentage
}

// WeightedVariant is a (name, weight) pair for variant assignment.
type WeightedVariant struct {
	Name   string
	Weight int
}

// AssignVariant assigns a user to a weighted variant: cumulative ranges over
// the 0-99 bucket, variants sorted by name (UTF-8 byte order — Go's native
// string comparison). It returns the variant name and true, or "" and false
// when the bucket falls beyond the total weight (base value applies).
func AssignVariant(flagKey, userKey string, variants []WeightedVariant) (string, bool) {
	sorted := make([]WeightedVariant, len(variants))
	copy(sorted, variants)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	bucket := RolloutBucket(flagKey, userKey)
	cumulative := 0
	for _, variant := range sorted {
		cumulative += variant.Weight
		if bucket < cumulative {
			return variant.Name, true
		}
	}
	return "", false
}

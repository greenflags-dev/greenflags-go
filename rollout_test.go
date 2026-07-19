package greenflags

import (
	"encoding/json"
	"os"
	"testing"
)

type vectorFile struct {
	Vectors []struct {
		FlagKey string `json:"flagKey"`
		UserKey string `json:"userKey"`
		Hash    uint32 `json:"hash"`
		Bucket  int    `json:"bucket"`
	} `json:"vectors"`
	VariantVectors []struct {
		FlagKey  string `json:"flagKey"`
		UserKey  string `json:"userKey"`
		Bucket   int    `json:"bucket"`
		Assigned *string `json:"assigned"`
		Variants []struct {
			Name   string `json:"name"`
			Weight int    `json:"weight"`
		} `json:"variants"`
	} `json:"variantVectors"`
}

func loadVectors(t *testing.T) vectorFile {
	t.Helper()
	raw, err := os.ReadFile("../rollout-test-vectors.json")
	if err != nil {
		t.Fatalf("reading vectors: %v", err)
	}
	var data vectorFile
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("parsing vectors: %v", err)
	}
	if len(data.Vectors) < 20 || len(data.VariantVectors) < 8 {
		t.Fatalf("unexpected vector counts: %d / %d", len(data.Vectors), len(data.VariantVectors))
	}
	return data
}

func TestRolloutBucketConformance(t *testing.T) {
	data := loadVectors(t)
	for _, vector := range data.Vectors {
		if got := fnv1a32(vector.FlagKey + ":" + vector.UserKey); got != vector.Hash {
			t.Errorf("hash(%q,%q) = %d, want %d", vector.FlagKey, vector.UserKey, got, vector.Hash)
		}
		if got := RolloutBucket(vector.FlagKey, vector.UserKey); got != vector.Bucket {
			t.Errorf("bucket(%q,%q) = %d, want %d", vector.FlagKey, vector.UserKey, got, vector.Bucket)
		}
	}
}

func TestIsIncludedInRolloutBoundaries(t *testing.T) {
	data := loadVectors(t)
	for _, vector := range data.Vectors {
		if IsIncludedInRollout(vector.FlagKey, vector.UserKey, 0) {
			t.Errorf("percentage 0 must never include (%q,%q)", vector.FlagKey, vector.UserKey)
		}
		if !IsIncludedInRollout(vector.FlagKey, vector.UserKey, 100) {
			t.Errorf("percentage 100 must always include (%q,%q)", vector.FlagKey, vector.UserKey)
		}
		if IsIncludedInRollout(vector.FlagKey, vector.UserKey, vector.Bucket) {
			t.Errorf("bucket == percentage must exclude (%q,%q)", vector.FlagKey, vector.UserKey)
		}
		if !IsIncludedInRollout(vector.FlagKey, vector.UserKey, vector.Bucket+1) {
			t.Errorf("bucket < percentage must include (%q,%q)", vector.FlagKey, vector.UserKey)
		}
	}
}

func TestAssignVariantConformance(t *testing.T) {
	data := loadVectors(t)
	for i, vector := range data.VariantVectors {
		weighted := make([]WeightedVariant, 0, len(vector.Variants))
		for _, variant := range vector.Variants {
			weighted = append(weighted, WeightedVariant{Name: variant.Name, Weight: variant.Weight})
		}
		name, assigned := AssignVariant(vector.FlagKey, vector.UserKey, weighted)
		if vector.Assigned == nil {
			if assigned {
				t.Errorf("vector %d: expected no assignment, got %q", i, name)
			}
			continue
		}
		if !assigned || name != *vector.Assigned {
			t.Errorf("vector %d: assign = %q (%v), want %q", i, name, assigned, *vector.Assigned)
		}
	}
}

func TestGetFlagForUser(t *testing.T) {
	client := NewClient(Options{URL: "https://example.com", APIToken: "t"})
	client.snapshot = map[string]Flag{
		// theme_name buckets: alice → 14, bob → 61.
		"theme_name": {
			Key:   "theme_name",
			Type:  FlagTypeString,
			Value: "base",
			Variants: []FlagVariant{
				{Name: "A", Weight: 30, Value: "azul"},
				{Name: "B", Weight: 70, Value: "verde"},
			},
		},
		"checkout_enabled": {
			Key:     "checkout_enabled",
			Type:    FlagTypeBoolean,
			Value:   true,
			Rollout: &Rollout{Percentage: 30},
		},
	}

	if value, _ := client.GetFlagForUser("theme_name", "alice"); value != "azul" {
		t.Errorf("alice variant = %v, want azul", value)
	}
	if value, _ := client.GetFlagForUser("theme_name", "bob"); value != "verde" {
		t.Errorf("bob variant = %v, want verde", value)
	}
	// checkout_enabled buckets: user-1 → 10 (in), user-2 → 91 (out).
	if !client.IsEnabledForUser("checkout_enabled", "user-1") {
		t.Error("user-1 must be inside the 30% rollout")
	}
	if client.IsEnabledForUser("checkout_enabled", "user-2") {
		t.Error("user-2 must be outside the 30% rollout")
	}
	// GetFlag without user: raw passthrough (fail-open).
	if value, _ := client.GetFlag("checkout_enabled"); value != true {
		t.Errorf("no-user read must pass through, got %v", value)
	}
}

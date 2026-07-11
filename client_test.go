package greenflags

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func flagsServer(t *testing.T, flags []map[string]any) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v1/flags" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer gf_test" {
			t.Errorf("unexpected auth header %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data":    map[string]any{"flags": flags},
		})
	}))
	t.Cleanup(server.Close)
	return server, &calls
}

func TestRefreshAndReads(t *testing.T) {
	server, _ := flagsServer(t, []map[string]any{
		{"key": "on", "type": "boolean", "value": true},
		{"key": "banner", "type": "string", "value": "hello"},
		{"key": "limit", "type": "number", "value": 42},
		{"key": "conf", "type": "json", "value": map[string]any{"theme": "dark"}},
	})

	client := NewClient(Options{URL: server.URL + "/", APIToken: "gf_test"})
	if err := client.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh failed: %v", err)
	}

	if !client.IsEnabled("on") {
		t.Error("expected on=true")
	}
	if got := client.StringFlag("banner", "def"); got != "hello" {
		t.Errorf("banner=%q", got)
	}
	if got := client.NumberFlag("limit", 0); got != 42 {
		t.Errorf("limit=%f", got)
	}
	if got := client.JSONFlag("conf", nil); got["theme"] != "dark" {
		t.Errorf("conf=%v", got)
	}
	// Missing flags fail open to defaults.
	if got := client.StringFlag("missing", "fallback"); got != "fallback" {
		t.Errorf("missing=%q", got)
	}
	if client.IsEnabled("missing") {
		t.Error("missing flag must not be enabled")
	}
	if _, ok := client.GetFlag("missing"); ok {
		t.Error("missing flag must report ok=false")
	}
	if len(client.AllFlags()) != 4 {
		t.Errorf("expected 4 flags, got %d", len(client.AllFlags()))
	}
}

func TestErrorEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"error":   "INVALID_TOKEN",
			"message": "Invalid API token.",
		})
	}))
	t.Cleanup(server.Close)

	client := NewClient(Options{URL: server.URL, APIToken: "bad"})
	err := client.Refresh(context.Background())

	var gfErr *Error
	if !errors.As(err, &gfErr) {
		t.Fatalf("expected *Error, got %v", err)
	}
	if gfErr.Code != "INVALID_TOKEN" || gfErr.Status != 401 {
		t.Errorf("unexpected error %+v", gfErr)
	}
}

func TestFailedRefreshKeepsPreviousSnapshot(t *testing.T) {
	var fail atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": false, "error": "QUOTA_EXCEEDED", "message": "Quota.",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": map[string]any{"flags": []map[string]any{
				{"key": "on", "type": "boolean", "value": true},
			}},
		})
	}))
	t.Cleanup(server.Close)

	client := NewClient(Options{URL: server.URL, APIToken: "gf_test"})
	if err := client.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh failed: %v", err)
	}

	fail.Store(true)
	if err := client.Refresh(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	if !client.IsEnabled("on") {
		t.Error("previous snapshot must survive a failed refresh")
	}
}

func TestGeofenceEvaluation(t *testing.T) {
	server, _ := flagsServer(t, []map[string]any{
		{
			"key": "geo-bool", "type": "boolean", "value": true,
			"geofence": map[string]any{"latitude": 19.4326, "longitude": -99.1332, "radiusMeters": 1000},
		},
		{
			"key": "geo-string", "type": "string", "value": "promo",
			"geofence": map[string]any{"latitude": 19.4326, "longitude": -99.1332, "radiusMeters": 1000},
		},
	})

	client := NewClient(Options{URL: server.URL, APIToken: "gf_test"})
	if err := client.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh failed: %v", err)
	}

	// No coordinates: geofence ignored.
	if !client.IsEnabled("geo-bool") {
		t.Error("no coords: expected true")
	}

	// Inside the radius.
	client.SetCoordinates(&Coordinates{Latitude: 19.4327, Longitude: -99.1333})
	if !client.IsEnabled("geo-bool") {
		t.Error("inside: expected true")
	}
	if got := client.StringFlag("geo-string", "def"); got != "promo" {
		t.Errorf("inside: geo-string=%q", got)
	}

	// Outside the radius (Monterrey, ~700km away).
	client.SetCoordinates(&Coordinates{Latitude: 25.6866, Longitude: -100.3161})
	if client.IsEnabled("geo-bool") {
		t.Error("outside: expected false")
	}
	if value, ok := client.GetFlag("geo-string"); !ok || value != nil {
		t.Errorf("outside: expected nil value, got %v ok=%v", value, ok)
	}

	// Clearing restores un-geofenced evaluation.
	client.SetCoordinates(nil)
	if !client.IsEnabled("geo-bool") {
		t.Error("cleared: expected true")
	}
}

func TestSubscribeAndUnsubscribe(t *testing.T) {
	server, _ := flagsServer(t, []map[string]any{
		{"key": "on", "type": "boolean", "value": true},
	})
	client := NewClient(Options{URL: server.URL, APIToken: "gf_test"})

	var notified atomic.Int64
	unsubscribe := client.Subscribe(func(snapshot map[string]Flag) {
		notified.Add(1)
		if snapshot["on"].Value != true {
			t.Error("snapshot must carry evaluated flags")
		}
	})

	if err := client.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	if notified.Load() != 1 {
		t.Fatalf("expected 1 notification, got %d", notified.Load())
	}

	unsubscribe()
	if err := client.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	if notified.Load() != 1 {
		t.Fatalf("expected no notification after unsubscribe, got %d", notified.Load())
	}
}

func TestPollingRefreshesAndStops(t *testing.T) {
	server, calls := flagsServer(t, []map[string]any{})
	client := NewClient(Options{URL: server.URL, APIToken: "gf_test"})

	client.StartPolling(context.Background(), 20*time.Millisecond)
	time.Sleep(90 * time.Millisecond)
	client.StopPolling()
	atStop := calls.Load()
	if atStop < 2 {
		t.Fatalf("expected >=2 polling calls, got %d", atStop)
	}

	time.Sleep(60 * time.Millisecond)
	if calls.Load() != atStop {
		t.Fatalf("polling did not stop: %d != %d", calls.Load(), atStop)
	}
}

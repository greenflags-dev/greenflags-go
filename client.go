package greenflags

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Options configures a Client. URL and APIToken are required.
type Options struct {
	// URL is the API base URL (e.g. https://app.greenflags.dev), trailing
	// slash optional.
	URL string
	// APIToken is the environment-scoped token (gf_...) created in the
	// dashboard. It determines which flags the client sees.
	APIToken string
	// Coordinates optionally sets the end-user location used for geofence
	// evaluation (see SetCoordinates).
	Coordinates *Coordinates
	// HTTPClient optionally overrides the HTTP client (custom timeouts,
	// proxies, or mocking). Defaults to a client with a 10s timeout.
	HTTPClient *http.Client
}

// Client reads GreenFlags feature flags. It is safe for concurrent use: one
// Client can be shared across goroutines. Every read path goes through
// geofence evaluation — the raw cached snapshot is never exposed.
type Client struct {
	url        string
	apiToken   string
	httpClient *http.Client

	mu          sync.RWMutex
	snapshot    map[string]Flag
	coordinates *Coordinates
	listeners   map[int]func(map[string]Flag)
	nextID      int
	pollCancel  context.CancelFunc
}

// NewClient builds a Client. It performs no network request — call Refresh
// to load the first snapshot.
func NewClient(opts Options) *Client {
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{
		url:         strings.TrimRight(strings.TrimSpace(opts.URL), "/"),
		apiToken:    opts.APIToken,
		httpClient:  httpClient,
		snapshot:    map[string]Flag{},
		coordinates: opts.Coordinates,
		listeners:   map[int]func(map[string]Flag){},
	}
}

// Refresh performs one billable request (GET /v1/flags), replaces the local
// snapshot and notifies subscribers. On error the previous snapshot is kept.
func (c *Client) Refresh(ctx context.Context) error {
	flags, err := requestFlags(ctx, c.httpClient, c.url, c.apiToken)
	if err != nil {
		return err
	}

	c.mu.Lock()
	c.snapshot = flags
	listeners := make([]func(map[string]Flag), 0, len(c.listeners))
	for _, fn := range c.listeners {
		listeners = append(listeners, fn)
	}
	evaluated := c.evaluatedSnapshotLocked()
	c.mu.Unlock()

	for _, fn := range listeners {
		fn(evaluated)
	}
	return nil
}

// Snapshot returns a copy of the evaluated snapshot, keyed by flag key.
// Local read — never hits the network.
func (c *Client) Snapshot() map[string]Flag {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.evaluatedSnapshotLocked()
}

// AllFlags returns every evaluated flag. Local read.
func (c *Client) AllFlags() []Flag {
	c.mu.RLock()
	defer c.mu.RUnlock()
	flags := make([]Flag, 0, len(c.snapshot))
	for _, flag := range c.snapshot {
		flags = append(flags, c.evaluateLocked(flag))
	}
	return flags
}

// GetFlag returns the evaluated value of key and whether the flag exists.
// Local read; never errors for missing flags.
func (c *Client) GetFlag(key string) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	flag, ok := c.snapshot[key]
	if !ok {
		return nil, false
	}
	return c.evaluateLocked(flag).Value, true
}

// IsEnabled returns true only when key exists and currently evaluates to true.
func (c *Client) IsEnabled(key string) bool {
	value, ok := c.GetFlag(key)
	return ok && value == true
}

// GetFlagForUser returns the evaluated value of key for a specific end user,
// resolving percentage rollout and variants with the given user key (see
// docs/rollout-hash-spec.md). Server processes serve many users, so identity
// is per call — there is no SetUser. Chain: geofence (client coordinates) →
// variants → rollout. Local read; never errors for missing flags.
func (c *Client) GetFlagForUser(key, user string) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	flag, ok := c.snapshot[key]
	if !ok {
		return nil, false
	}
	return c.evaluateForUserLocked(flag, user).Value, true
}

// IsEnabledForUser returns true only when key exists and evaluates to true
// for the given user.
func (c *Client) IsEnabledForUser(key, user string) bool {
	value, ok := c.GetFlagForUser(key, user)
	return ok && value == true
}

// BoolFlag returns the flag's value as a bool, or def when the flag is
// missing or not a bool.
func (c *Client) BoolFlag(key string, def bool) bool {
	if value, ok := c.GetFlag(key); ok {
		if b, isBool := value.(bool); isBool {
			return b
		}
	}
	return def
}

// StringFlag returns the flag's value as a string, or def when the flag is
// missing or not a string.
func (c *Client) StringFlag(key string, def string) string {
	if value, ok := c.GetFlag(key); ok {
		if s, isString := value.(string); isString {
			return s
		}
	}
	return def
}

// NumberFlag returns the flag's value as a float64 (JSON numbers), or def
// when the flag is missing or not a number.
func (c *Client) NumberFlag(key string, def float64) float64 {
	if value, ok := c.GetFlag(key); ok {
		if n, isNumber := value.(float64); isNumber {
			return n
		}
	}
	return def
}

// JSONFlag returns the flag's value as a map, or def when the flag is
// missing or not a JSON object.
func (c *Client) JSONFlag(key string, def map[string]any) map[string]any {
	if value, ok := c.GetFlag(key); ok {
		if m, isMap := value.(map[string]any); isMap {
			return m
		}
	}
	return def
}

// Subscribe registers fn, called with the evaluated snapshot after every
// successful Refresh. It returns an unsubscribe function.
func (c *Client) Subscribe(fn func(map[string]Flag)) (unsubscribe func()) {
	c.mu.Lock()
	id := c.nextID
	c.nextID++
	c.listeners[id] = fn
	c.mu.Unlock()

	return func() {
		c.mu.Lock()
		delete(c.listeners, id)
		c.mu.Unlock()
	}
}

// StartPolling refreshes every interval on a background goroutine until
// StopPolling is called or ctx is canceled. Errors are swallowed: the
// previous snapshot stays available. Every tick is one billable request.
func (c *Client) StartPolling(ctx context.Context, interval time.Duration) {
	c.StopPolling()

	pollCtx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	c.pollCancel = cancel
	c.mu.Unlock()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-pollCtx.Done():
				return
			case <-ticker.C:
				_ = c.Refresh(pollCtx)
			}
		}
	}()
}

// StopPolling stops the polling goroutine. The in-memory snapshot is kept.
func (c *Client) StopPolling() {
	c.mu.Lock()
	cancel := c.pollCancel
	c.pollCancel = nil
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// SetCoordinates sets (or clears, with nil) the end-user location used for
// geofence evaluation. No network request is made.
func (c *Client) SetCoordinates(coords *Coordinates) {
	c.mu.Lock()
	c.coordinates = coords
	c.mu.Unlock()
}

func (c *Client) evaluatedSnapshotLocked() map[string]Flag {
	evaluated := make(map[string]Flag, len(c.snapshot))
	for key, flag := range c.snapshot {
		evaluated[key] = c.evaluateLocked(flag)
	}
	return evaluated
}

func (c *Client) evaluateLocked(flag Flag) Flag {
	if c.coordinates == nil || flag.Geofence == nil {
		return flag
	}
	center := Coordinates{Latitude: flag.Geofence.Latitude, Longitude: flag.Geofence.Longitude}
	outside := GeoDistanceMeters(*c.coordinates, center) > flag.Geofence.RadiusMeters
	if !outside {
		return flag
	}
	return offValue(flag)
}

func offValue(flag Flag) Flag {
	if flag.Type == FlagTypeBoolean {
		flag.Value = false
	} else {
		flag.Value = nil
	}
	return flag
}

// evaluateForUserLocked applies the full chain per docs/rollout-hash-spec.md:
// geofence first, then variants/rollout with the per-call user key.
func (c *Client) evaluateForUserLocked(flag Flag, user string) Flag {
	if c.coordinates != nil && flag.Geofence != nil {
		center := Coordinates{Latitude: flag.Geofence.Latitude, Longitude: flag.Geofence.Longitude}
		if GeoDistanceMeters(*c.coordinates, center) > flag.Geofence.RadiusMeters {
			return offValue(flag)
		}
	}

	if len(flag.Variants) > 0 {
		weighted := make([]WeightedVariant, 0, len(flag.Variants))
		for _, variant := range flag.Variants {
			weighted = append(weighted, WeightedVariant{Name: variant.Name, Weight: variant.Weight})
		}
		name, assigned := AssignVariant(flag.Key, user, weighted)
		if !assigned {
			return flag // beyond total weight → base value
		}
		for _, variant := range flag.Variants {
			if variant.Name == name {
				flag.Value = variant.Value
				return flag
			}
		}
		return flag
	}

	if flag.Rollout != nil && !IsIncludedInRollout(flag.Key, user, flag.Rollout.Percentage) {
		return offValue(flag)
	}

	return flag
}

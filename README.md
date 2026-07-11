# greenflags-go

Official Go SDK for consuming **GreenFlags feature flags** from any Go service: HTTP servers, gRPC services, CLIs, workers.

**Zero dependencies** — standard library only. Built to minimize billable requests: one network call fetches the whole environment; every read after that is served from memory. Safe for concurrent use across goroutines.

> **Status:** `v0.1.0`. Full changelog in [`CHANGELOG.md`](./CHANGELOG.md).

```sh
go get github.com/greenflags-dev/greenflags-go
```

---

## Table of Contents

- [Why it exists](#why-it-exists)
- [Features](#features)
- [Requirements](#requirements)
- [Quick Start](#quick-start)
- [Usage Guide](#usage-guide)
- [API Reference](#api-reference)
- [Geofence](#geofence)
- [Error Handling](#error-handling)
- [Billing Model](#billing-model)
- [Handling the API token](#handling-the-api-token)
- [Development](#development)
- [Versioning](#versioning)

---

## Why it exists

GreenFlags exposes a read endpoint (`GET /v1/flags`) where **every 2xx response counts as a billable read**. A naive integration that fetches a flag on every request handler can generate thousands of unnecessary requests and burn through your quota for no reason.

`greenflags-go` solves this with a **snapshot + cache** model:

1. One call (`Refresh`) fetches **every** flag in the `project + environment` tied to your API token.
2. That response is stored in memory.
3. Every read after that (`IsEnabled`, `BoolFlag`, `StringFlag`, …) is local — **zero additional requests**.

There is intentionally no method to fetch a single flag over the network — that would break the billing model.

## Features

- ✅ Zero dependencies — `net/http` and the standard library, nothing else.
- ✅ Snapshot + in-memory cache — billing-safe by design.
- ✅ Concurrency-safe — share one `*Client` across goroutines (`sync.RWMutex` inside).
- ✅ `context.Context` on every network call — timeouts and cancellation the Go way.
- ✅ Opt-in polling on a background goroutine — you decide if and how often it refreshes.
- ✅ Fail-open — a failed refresh keeps the last good snapshot; typed accessors return your defaults.
- ✅ Client-side geofence evaluation — the end-user's location never leaves your process.
- ✅ Injectable `*http.Client` — custom timeouts, proxies, or `httptest` mocking.

## Requirements

- **Go 1.22+**.
- A GreenFlags **API token**, generated from the [dashboard](https://app.greenflags.dev) for a specific `project + environment`. The token determines which flags the SDK sees. See the [API docs](https://greenflags.dev/docs/) for the full contract.

## Quick Start

```go
package main

import (
	"context"

	greenflags "github.com/greenflags-dev/greenflags-go"
)

func main() {
	flags := greenflags.NewClient(greenflags.Options{
		URL:      "https://app.greenflags.dev",
		APIToken: "gf_your_token_here",
	})

	if err := flags.Refresh(context.Background()); err != nil {
		// fail-open: keep going with defaults
	}

	if flags.IsEnabled("new-checkout") {
		// ship it
	}
}
```

## Usage Guide

```go
flags := greenflags.NewClient(greenflags.Options{URL: url, APIToken: token})

// 1. Fetch the initial snapshot (required before reading real flag values)
if err := flags.Refresh(ctx); err != nil { /* log it */ }

// 2. Read flags — always from memory, never hits the network
enabled := flags.IsEnabled("my-feature")             // bool sugar
theme   := flags.StringFlag("theme", "light")        // string with default
limit   := flags.NumberFlag("rate-limit", 100)       // float64 with default
conf    := flags.JSONFlag("config", nil)             // map[string]any with default
value, ok := flags.GetFlag("anything")               // raw value + existence

// 3. List everything available
all      := flags.AllFlags()   // []Flag
snapshot := flags.Snapshot()   // map[string]Flag

// 4. Subscribe to updates (fires on every successful Refresh)
unsubscribe := flags.Subscribe(func(snapshot map[string]greenflags.Flag) {
	// react to changes
})
defer unsubscribe()

// 5. Opt-in polling — without this, the SDK NEVER fetches data on its own
flags.StartPolling(ctx, 60*time.Second) // every tick = 1 billable request
defer flags.StopPolling()
```

### Ground rules

- Typed accessors (`BoolFlag`, `StringFlag`, `NumberFlag`, `JSONFlag`) **never panic or error** — they return your default when the flag is missing *or* holds a different type.
- `Refresh` **can fail** (`*greenflags.Error`: network error, invalid token, quota exceeded) — the previous snapshot is kept either way.
- Create **one client per process** and share it — don't build a client (or call `Refresh`) per request. Refresh once at startup and use `StartPolling` only if you need near-live data.
- JSON numbers arrive as `float64` (standard `encoding/json` behavior) — that's what `NumberFlag` returns.

## API Reference

### `NewClient(Options) *Client`

| Option | Type | Description |
|---|---|---|
| `URL` | `string` | API base URL, trailing slash optional. |
| `APIToken` | `string` | Environment-scoped token (`gf_...`). |
| `Coordinates` | `*Coordinates` | Optional end-user location for geofence evaluation. |
| `HTTPClient` | `*http.Client` | Optional override; defaults to a 10s-timeout client. |

### Methods

| Method | Signature | Description |
|---|---|---|
| `Refresh` | `(ctx) error` | 1 request to `GET /v1/flags`. Replaces the snapshot and notifies subscribers. |
| `Snapshot` | `() map[string]Flag` | Copy of the evaluated snapshot. Local read. |
| `AllFlags` | `() []Flag` | Every evaluated flag. Local read. |
| `GetFlag` | `(key) (any, bool)` | Evaluated value + whether the flag exists. |
| `IsEnabled` | `(key) bool` | `true` only when the flag exists and evaluates to `true`. |
| `BoolFlag` / `StringFlag` / `NumberFlag` / `JSONFlag` | `(key, def) T` | Typed accessors with defaults. Never error. |
| `Subscribe` | `(fn) (unsubscribe func())` | Listener on every successful `Refresh`. |
| `StartPolling` | `(ctx, interval)` | Background `Refresh` loop. Opt-in. Fail-open. |
| `StopPolling` | `()` | Stops the loop; snapshot preserved. |
| `SetCoordinates` | `(*Coordinates)` | Sets/clears the location for geofence evaluation. No network. |

## Geofence

Some flags carry an optional geofence (latitude/longitude/radius, configured in the dashboard). With coordinates set, each geofenced flag is evaluated **locally** — coordinates never leave the process:

- **Inside the radius** (on-edge counts as inside): the flag's normal value.
- **Outside**: the off value — `false` for `boolean` flags, `nil` for the rest.
- **No coordinates, or no geofence**: the normal value, unaffected.

> Fail-open, by design: a geofence is not a security boundary — any caller that omits coordinates sees the flag's normal value regardless of location.

```go
flags.SetCoordinates(&greenflags.Coordinates{Latitude: 19.4326, Longitude: -99.1332})
flags.IsEnabled("store-promo") // evaluated against the geofence, if any
flags.SetCoordinates(nil)      // back to "ignore geofence"
```

## Error Handling

`Refresh` returns a `*greenflags.Error` with `Code`, `Message` and HTTP `Status`:

```go
var gfErr *greenflags.Error
if err := flags.Refresh(ctx); errors.As(err, &gfErr) {
	log.Printf("flags refresh failed: %s (%d)", gfErr.Code, gfErr.Status)
}
```

Codes that `GET /v1/flags` can actually return:

| `Code` | `Status` | Cause |
|---|---|---|
| `INVALID_TOKEN` | 401 | Token missing, invalid, or revoked |
| `QUOTA_EXCEEDED` | 429 | Monthly read quota exhausted |
| `BILLING_NO_SUBSCRIPTION` | 429 | The workspace has no active subscription |
| `BILLING_CANCELED` | 429 | Subscription canceled |
| `BILLING_PAST_DUE` | 429 | Payment past due |
| `BILLING_TRIAL_EXPIRED` | 429 | Trial expired |
| `BILLING_LIMIT_REACHED` | 429 | Billing limit reached |
| `NETWORK_ERROR` | 0 | The request failed before a response was received |
| `PARSE_ERROR` | response status | Body wasn't valid JSON, or was missing `data.flags` |
| `REQUEST_ERROR` | response status | Non-2xx response with no parseable error code |

Read methods **never** return any of these — they're always local.

## Billing Model

Every `Refresh` (manual or polling tick) is **exactly one HTTP request**, and every 2xx response counts as one billable read. All flag reads are 100% in memory. With N instances each polling, you pay N reads per tick — size your interval and instance count accordingly.

## Handling the API token

Server-side Go: treat the token like a database password.

- Read it from the environment (`os.Getenv("GREENFLAGS_API_TOKEN")`), never hardcode it.
- Per-token **monthly quotas** (dashboard → API Tokens) cap the blast radius of a leaked CI log or misconfigured staging.

## Development

```sh
cd sdks/go
go vet ./...
go test ./...   # 10 tests against httptest servers — no real network
```

## Versioning

Semver via git tags (`v0.1.0`, …). While in `v0.x`, `MINOR` can include API changes; `PATCH` are fixes. Version-by-version detail in [`CHANGELOG.md`](./CHANGELOG.md).

## Related

- [API reference](https://greenflags.dev/docs/)
- [`@greenflags/client`](https://www.npmjs.com/package/@greenflags/client) — JavaScript/TypeScript SDK
- [`@greenflags/react`](https://www.npmjs.com/package/@greenflags/react) — React hooks
- [`greenflags`](https://pub.dev/packages/greenflags) — Dart/Flutter SDK
- [`greenflags` on PyPI](https://pypi.org/project/greenflags/) — Python SDK
- [`@greenflags/mcp`](https://www.npmjs.com/package/@greenflags/mcp) — MCP server for AI agents

# Changelog

Format based on [Keep a Changelog](https://keepachangelog.com/). Versioning: [SemVer](https://semver.org/) via git tags (while in `v0.x`, a MINOR release may include breaking changes; strict semver applies from `v1.0.0` onward).

## [0.3.0] - 2026-07-18

### Added
- Percentage rollout and multivariate evaluation with per-call user identity: `GetFlagForUser(key, user)` and `IsEnabledForUser(key, user)` resolve a flag's `rollout`/`variants` rules for a specific end user (server processes serve many users, so identity is per call — there is no SetUser). Chain: geofence → variants → rollout, per `docs/rollout-hash-spec.md`; conformance locked by `sdks/rollout-test-vectors.json`.
- `Rollout`, `FlagVariant`, `WeightedVariant` types and `RolloutBucket`/`IsIncludedInRollout`/`AssignVariant` exported.
- `Flag` gains `Rollout`/`Variants` fields (raw config visible on no-user reads; `GetFlag` passes through unchanged, fail-open).

## [0.2.0] - 2026-07-11

### Added
- `GeoDistanceMeters(a, b Coordinates) float64` — exported from the package. Returns the great-circle distance in meters between two `Coordinates`, the same haversine calculation the SDK runs internally for geofence evaluation. Lets callers show the live distance to a geofence without reimplementing it. No behavior change to flag evaluation.

## [0.1.0] - 2026-07-10

### Added
- Initial release, mirroring `@greenflags/client` 0.2.x semantics in pure Go (1.22+, zero dependencies).
- `Client` with `Refresh(ctx)`, `Snapshot`, `AllFlags`, `GetFlag` (value + ok), `IsEnabled`, typed accessors with defaults (`BoolFlag`/`StringFlag`/`NumberFlag`/`JSONFlag`), `Subscribe`, `StartPolling`/`StopPolling` (context-aware goroutine, fail-open), `SetCoordinates`.
- Concurrency-safe snapshot access (`sync.RWMutex`) — one client shared across goroutines.
- Client-side geofence evaluation (haversine): outside the radius a `boolean` flag evaluates to `false`, other types to `nil`. Every read path goes through evaluation — the raw snapshot is never exposed.
- Typed `*Error` with API error `Code`, `Message` and HTTP `Status` (`NETWORK_ERROR`/`PARSE_ERROR` client-side).
- Injectable `*http.Client` (default 10s timeout), auth via `Authorization: Bearer` against `GET /v1/flags`.

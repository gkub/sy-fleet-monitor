# internal/handler

This package is the **HTTP layer** - the bridge between the outside world (HTTP requests) and the internal logic (device data and metric calculations).

It owns JSON parsing/encoding, HTTP status codes, URL path values, and error responses. Device storage and metric calculations stay in their own packages.

---

## Overview

```mermaid
flowchart LR
    Request[Incoming\nHTTP Request] --> LookupDevice

    subgraph handler package
        LookupDevice[lookupDevice\nExtract device_id from URL\nFind device in registry] -->|not found| Error404[writeError\n404 Not Found]
        LookupDevice -->|found| Decode[Decode JSON body]
        Decode -->|bad JSON| Error400[writeError\n400 Bad Request]
        Decode -->|valid| Action

        Action -->|POST heartbeat| RecordHB[device.RecordHeartbeat]
        Action -->|POST stats| RecordStat[device.RecordUploadStat]
        Action -->|GET stats| Snapshot[device.Snapshot\nthen metrics.Calculate...]

        RecordHB --> R204A[204 No Content]
        RecordStat --> R204B[204 No Content]
        Snapshot --> R200[200 OK\nJSON response]
    end
```

---

## Request and Response Types

These structs define the shape of JSON bodies. JSON field names follow the OpenAPI contract.

### `heartbeatRequest`
The JSON body the device sends to `POST .../heartbeat`.

| Field | JSON key | Type | Meaning |
|-------|----------|------|---------|
| `SentAt` | `sent_at` | `time.Time` | When the device generated this heartbeat |

### `uploadStatsRequest`
The JSON body the device sends to `POST .../stats`.

| Field | JSON key | Type | Meaning |
|-------|----------|------|---------|
| `SentAt` | `sent_at` | `time.Time` | When the device sent this report |
| `UploadTime` | `upload_time` | `int64` | How long the last video upload took, in nanoseconds |

### `deviceStatsResponse`
The JSON body returned by `GET .../stats`.

| Field | JSON key | Type | Meaning |
|-------|----------|------|---------|
| `Uptime` | `uptime` | `float64` | Availability percentage, e.g. `98.75` |
| `AvgUploadTime` | `avg_upload_time` | `string` | Mean upload duration, e.g. `"3m17.331667813s"` |

### `errorResponse`
The JSON body returned with any 4xx or 5xx response.

| Field | JSON key | Type | Meaning |
|-------|----------|------|---------|
| `Message` | `msg` | `string` | Human-readable description of what went wrong |

The OpenAPI contract requires the error key to be `msg` specifically - which is why this struct has a `json:"msg"` tag rather than using the default field name.

---

## Handler Functions

Each handler accepts a `*device.Registry` and returns an `http.HandlerFunc`. The returned function captures the registry from its enclosing scope, so it has access to device data on every request without relying on global state. Each handler is called once at startup to produce the function stored in the router.

### `RecordHeartbeat(deviceRegistry *device.Registry) http.HandlerFunc`

**What it does:** Handles `POST /api/v1/devices/{device_id}/heartbeat`.

Looks up the target device, decodes the heartbeat timestamp, stores it, and responds `204 No Content`.

---

### `RecordUploadStats(deviceRegistry *device.Registry) http.HandlerFunc`

**What it does:** Handles `POST /api/v1/devices/{device_id}/stats`.

Looks up the target device, decodes the upload duration, stores it, and responds `204 No Content`.

---

### `GetDeviceStats(deviceRegistry *device.Registry) http.HandlerFunc`

**What it does:** Handles `GET /api/v1/devices/{device_id}/stats`.

This handler snapshots the device telemetry, computes the current metrics, and returns them as JSON.

```mermaid
sequenceDiagram
    participant R as HTTP Request
    participant H as GetDeviceStats handler
    participant D as device.Device
    participant M as metrics package
    participant W as HTTP Response

    R->>H: GET /api/v1/devices/{id}/stats
    H->>D: lookupDevice → FindByID
    D-->>H: *Device
    H->>D: Snapshot()
    Note over D: Lock → copy slices → unlock
    D-->>H: heartbeatTimestamps, uploadTimes
    H->>M: CalculateUptime(heartbeatTimestamps)
    M-->>H: 99.79167
    H->>M: CalculateAverageUploadDuration(uploadTimes)
    M-->>H: 3m29.226522788s
    H->>W: writeJSON 200 OK\n{"uptime": 99.79, "avg_upload_time": "3m29s..."}
```

The response uses the uptime percentage and the duration string format required by the OpenAPI contract.

---

## Shared Helper Functions

Internal utilities used by the three handlers above.

### `lookupDevice(...) (*device.Device, bool)`

**What it does:** Extracts the `{device_id}` from the URL (using `r.PathValue("device_id")`), looks it up in the registry, and writes a `404` JSON error if it isn't found.

The `bool` return signals success or failure. If it returns `false`, the error response has already been written - the calling handler just needs to `return`.

### `writeJSON(responseWriter, statusCode, responseBody)`

Sets `Content-Type: application/json`, writes the status code, then encodes the response body as JSON.

### `writeError(responseWriter, statusCode, message)`

Thin wrapper around `writeJSON` that formats the message as `{"msg": "..."}`, the shape the OpenAPI contract requires for all error responses.

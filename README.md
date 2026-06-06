# Fleet Stats Service

An HTTP server that receives telemetry from a fleet of edge devices and exposes per-device uptime and average upload time statistics. Built as a submission for the SafelyYou Monitoring the Fleet coding challenge.

## Contents

- [How to Run](#how-to-run)
- [API Endpoints](#api-endpoints)
- [Project Structure](#project-structure)
- [Package READMEs](#package-readmes)
- [Architecture Overview](#architecture-overview)
- [Request Lifecycle](#request-lifecycle)
- [Simulator Results](#simulator-results)
- [Solution Write-Up](#solution-write-up)

---

## How to Run

```bash
# From the repo root
go run ./cmd/fleetstats -devices devices.csv

# Then, in a second terminal, run the device simulator
./device-simulator-linux-amd64 --port 6733
```

The server listens on port **6733** by default (as required by the OpenAPI contract). Results are written to `results.txt`.

## API Endpoints

| Method | Path | What it does |
|--------|------|-------------|
| `POST` | `/api/v1/devices/{device_id}/heartbeat` | Device reports that it is alive |
| `POST` | `/api/v1/devices/{device_id}/stats` | Device reports a video upload duration |
| `GET`  | `/api/v1/devices/{device_id}/stats`  | Query computed uptime % and avg upload time |

---

## Project Structure

```
sy_code_challenge/
├── cmd/
│   └── fleetstats/
│       └── main.go          ← entry point: startup, flags, routing
├── internal/
│   ├── device/
│   │   └── device.go        ← data model: Device struct + in-memory Registry
│   ├── metrics/
│   │   └── metrics.go       ← pure math: uptime and avg upload time calculations
│   └── handler/
│       └── handler.go       ← HTTP layer: JSON in/out, routing helpers, error responses
├── devices.csv              ← list of known device IDs
├── openapi.json             ← API contract
└── results.txt              ← simulator output from last run
```

---

## Package READMEs

Each package has its own README covering package responsibilities and notable implementation details.

| Package | Description | README |
|---------|-------------|--------|
| `cmd/fleetstats` | Entry point: startup, flags, routing | [cmd/fleetstats/README.md](cmd/fleetstats/README.md) |
| `internal/device` | Data model: Device struct, in-memory Registry, all locking | [internal/device/README.md](internal/device/README.md) |
| `internal/metrics` | Pure math: uptime and avg upload time calculations | [internal/metrics/README.md](internal/metrics/README.md) |
| `internal/handler` | HTTP layer: request handling, JSON, error responses | [internal/handler/README.md](internal/handler/README.md) |

---

## Architecture Overview

This diagram shows how the four packages depend on each other. Arrows mean "imports / calls into."

```mermaid
graph TD
    A["cmd/fleetstats\n(main.go)\n\nEntry point.\nReads CSV, wires up routes,\nstarts HTTP server."]

    B["internal/handler\n\nHTTP request handling,\nJSON encoding/decoding,\nerror responses."]

    C["internal/device\n\nDevice model,\nin-memory registry,\nlocking."]

    D["internal/metrics\n\nUptime and average upload\ntime calculations."]

    A -->|"creates Registry,\npasses to handlers"| B
    A -->|"creates Registry"| C
    B -->|"calls FindByID,\nRecordHeartbeat,\nRecordUploadStat,\nSnapshot"| C
    B -->|"calls CalculateUptime,\nCalculateAverageUploadDuration"| D
```

---

## Request Lifecycle

### POST /heartbeat - device checks in

```mermaid
sequenceDiagram
    participant Simulator as Device Simulator
    participant Handler as handler.RecordHeartbeat
    participant Registry as device.Registry
    participant Device as device.Device

    Simulator->>Handler: POST /api/v1/devices/{id}/heartbeat\n{"sent_at": "2024-01-01T10:04:00Z"}
    Handler->>Registry: FindByID("60-6b-44-84-dc-64")
    Registry-->>Handler: *Device (found)
    Handler->>Handler: Decode JSON body → heartbeatRequest
    Handler->>Device: RecordHeartbeat(sent_at)
    Handler-->>Simulator: 204 No Content
```

### POST /stats - device reports upload duration

```mermaid
sequenceDiagram
    participant Simulator as Device Simulator
    participant Handler as handler.RecordUploadStats
    participant Registry as device.Registry
    participant Device as device.Device

    Simulator->>Handler: POST /api/v1/devices/{id}/stats\n{"sent_at": "...", "upload_time": 197331667813}
    Handler->>Registry: FindByID(id)
    Registry-->>Handler: *Device (found)
    Handler->>Handler: Decode JSON body → uploadStatsRequest
    Handler->>Device: RecordUploadStat(upload_time)
    Handler-->>Simulator: 204 No Content
```

### GET /stats - simulator queries results

```mermaid
sequenceDiagram
    participant Simulator as Device Simulator
    participant Handler as handler.GetDeviceStats
    participant Registry as device.Registry
    participant Device as device.Device
    participant Metrics as metrics package

    Simulator->>Handler: GET /api/v1/devices/{id}/stats
    Handler->>Registry: FindByID(id)
    Registry-->>Handler: *Device (found)
    Handler->>Device: Snapshot()
    Device-->>Handler: []time.Time heartbeats, []int64 uploadTimes
    Handler->>Metrics: CalculateUptime(heartbeats)
    Metrics-->>Handler: 99.79167
    Handler->>Metrics: CalculateAverageUploadDuration(uploadTimes)
    Metrics-->>Handler: 3m29.226522788s
    Handler-->>Simulator: 200 OK\n{"uptime": 99.79167, "avg_upload_time": "3m29.226522788s"}
```

---

## Simulator Results

The device simulator matched expected uptime and average upload-time values for all five devices. Full simulator output is in `results.txt`.

## Solution Write-Up

The assignment write-up is in [SOLUTION_WRITEUP.md](SOLUTION_WRITEUP.md).

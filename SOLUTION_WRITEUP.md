# Solution Write-Up

### How long did you spend working on the problem? What was the most difficult part?

I spent roughly 20 hours getting the project to its current, submission-ready state. There are still small features and optimizations I could add, but the service is complete and passes the simulator.

As discussed in my first interview, I had never worked with (nor seen a line of) Go prior to this assignment. Given that context, the hardest part was learning enough Go to start building confidently. The underlying engineering concepts were familiar from work, university, and personal projects: I've worked with REST APIs, request/response handling, in-memory storage/pointers, and synchronization primitives in a variety of contexts and languages over the past 8+ years. The only challenge has been translating those concepts into a language/standard library I am learning as I go (pun unintended).

In terms of AI tooling, I used both ChatGPT and Claude (plus Google, but mainly the search engine as opposed to Gemini) to find Go documentation, standard-library APIs, similar projects, and best-practice resources to study before implementing. I also used AI coding agents to help scaffold and implement parts of the code, compare design approaches, and to review the project for edge cases and clarity. I manually read through documentation, forums, articles, and various AI-recommended Go resources, fact-checked uncertain claims, cross-checked the implementation against the OpenAPI contract and simulator, and iterated on the code, tests, and documentation until the project was solidly correct, readable, and ready to submit.

### How would you modify the data model to support more metric types?

The current `Device` struct has two separate histories: one for heartbeats and one for upload times. That works for the two required metrics, but any new metric would need another field on `Device`, another recording method, another handler, and another calculation function.

One way to tackle this would be to make the device store each metric using a standardized record format instead. Each metric would still have a name, like `"heartbeat"`, `"upload_time"`, or `"battery_level"`, but the stored record would always look the same: when it was recorded, and what value was recorded. For trivial cases, this at least helps scale our our metric recording handling.

```go
type MetricSample struct {
    RecordedAt time.Time
    Value      float64
}

type Device struct {
    ID      string
    metrics map[string][]MetricSample // "heartbeat", "upload_time", "battery_level", etc.
    mu      sync.Mutex
}
```

The code could expose a small helper like this:

```go
func (d *Device) RecordMetric(name string, value float64, recordedAt time.Time) {
    d.metrics[name] = append(d.metrics[name], MetricSample{
        RecordedAt: recordedAt,
        Value:      value,
    })
}
```

Then the specific application code can stay readable:

```go
device.RecordMetric("heartbeat", 1, receivedAt)
device.RecordMetric("upload_time", uploadTime, receivedAt)
device.RecordMetric("battery_level", batteryLevel, receivedAt)
```

Adding a new metric would still require the route, validation, and calculation logic for that metric. The difference is that the `Device` struct stays unchanged.

**The larger, more sensible long-term change would be to move to a database.** In-memory storage works fine for a small fleet over a short window (or perhaps on a supercomputer for a specialized small fleet), but it doesn't scale. At one heartbeat per minute per device, 30,000+ devices running for a year would need hundreds of gigabytes of RAM just for heartbeat data alone.

*(As a side note - the "30,000" number I'm referencing herein is just based on the number we discussed in our initial interview).*

PostgreSQL would be my first choice - partly because it handles timestamped device measurements well and scales comfortably to millions of rows with proper indexing, but honestly also because it's what I know best. I've built a multi-tenant Postgres database for fleet edge-AI devices in my current role, so this domain feels familiar. I'd be open to a better fit if one exists for the specific requirements, but Postgres is where I'd start. The result is the same query capability with a fraction of the memory footprint, and adding a new metric type becomes as simple as a new table and a new route - existing code is untouched.

### Runtime Complexity

| Endpoint | Time | Notes |
|----------|------|-------|
| `POST /heartbeat` | O(1) amortized | Appends one heartbeat sample |
| `POST /stats` | O(1) amortized | Appends one upload-time sample |
| `GET /stats` | O(H + U) | H = heartbeat count, U = upload count |

The real scaling concern is memory, not request time. The write endpoints append to slices, so they are O(1) amortized. The read endpoint is O(H + U) because it copies and scans all heartbeat and upload samples for a device.

Storing every raw heartbeat forever means memory per device grows without bound: 1 heartbeat/min × 1 year × 24 bytes = roughly 12 MB per device. Across 30,000 devices, that is more than 350 GB in one year. The production fix is to store running aggregates instead of raw slices:

```go
// O(1) memory per device regardless of how long it has been running
type DeviceStats struct {
    MinutesWithHeartbeat   int64
    TotalMinutesObserved   int64
    TotalUploadNanoseconds int64
    UploadCount            int64
}
```

`GET /stats` then becomes O(1): two divisions rather than iterating over slices. The tradeoff is losing the ability to recompute with a changed formula or query historical windows.

### On Concurrency and Scale

The locking design is fine for the expected scale:

- **Per-device mutexes** mean writes to different devices never compete. Devices can receive telemetry simultaneously.
- **`sync.RWMutex` on the Registry** means stat queries can run in parallel; only CSV loading needs exclusive access.
- **Snapshot-then-compute** keeps the lock held only for a memory copy, never for the math. This keeps lock contention low.

The only structural bottleneck at very high device counts would be the initial `LoadFromCSV` write lock, which is held while populating the map. With 30,000 entries, this is not a concern in practice.

// The device package defines the Device data model and the in-memory Registry
// that stores all known devices and their recorded telemetry.
//
// All exported methods are safe to call from multiple goroutines simultaneously.
// The Registry uses a read/write mutex so many concurrent reads (e.g. the
// simulator querying stats for all devices at once) never block each other.
// Each Device has its own separate mutex so telemetry writes for one device
// never compete with writes for a different device. This per-device locking
// strategy is what keeps the design scalable to tens of thousands of devices.
package device

import (
	"encoding/csv"
	"fmt"
	"os"
	"sync"
	"time"
)

/* -------------------------------------------------------------- */
// Device
/* -------------------------------------------------------------- */

// Device holds all in-memory state for a single monitored device.
//
// The data fields (heartbeatTimestamps, uploadTimesNanoseconds) are intentionally
// unexported - lowercase names in Go mean "private to this package." Callers in
// other packages must use the RecordHeartbeat, RecordUploadStat, and Snapshot
// methods, which ensures the mutex is always held correctly around every read
// and write. This is the encapsulation pattern in Go.
type Device struct {
	// ID is the unique string identifier for this device, as it appears in devices.csv.
	ID string

	// heartbeatTimestamps is the full ordered list of times this device checked in.
	// One heartbeat is expected per minute; any missing minute means the device was offline.
	heartbeatTimestamps []time.Time

	// uploadTimesNanoseconds stores each video upload duration reported by this device,
	// measured in nanoseconds. These are averaged to compute the avg_upload_time stat.
	uploadTimesNanoseconds []int64

	// mu is a mutex (mutual exclusion lock). It ensures that if two HTTP requests
	// for the same device arrive at exactly the same time, they take turns writing
	// rather than corrupting each other's data.
	mu sync.Mutex
}

// RecordHeartbeat appends a heartbeat timestamp to this device's history.
// It acquires the device's lock before writing and releases it when done.
func (d *Device) RecordHeartbeat(sentAt time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock() // defer guarantees this runs even if something unexpected happens

	d.heartbeatTimestamps = append(d.heartbeatTimestamps, sentAt)
}

// RecordUploadStat appends a video upload duration to this device's history.
// uploadTimeNanoseconds is the raw nanosecond count reported by the device.
func (d *Device) RecordUploadStat(uploadTimeNanoseconds int64) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.uploadTimesNanoseconds = append(d.uploadTimesNanoseconds, uploadTimeNanoseconds)
}

// Snapshot returns independent copies of the device's heartbeat and upload data.
//
// The key idea: we hold the lock only long enough to copy the slices, then
// release it. The caller can take as long as it wants doing math on the copies
// without blocking any incoming telemetry writes. Never share a live slice
// between a writer and a reader - always copy first.
func (d *Device) Snapshot() (heartbeatTimestamps []time.Time, uploadTimesNanoseconds []int64) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// make() allocates a new slice of exactly the right length.
	// copy() fills it with the current values. The result is independent of d's internal slice.
	heartbeatTimestamps = make([]time.Time, len(d.heartbeatTimestamps))
	copy(heartbeatTimestamps, d.heartbeatTimestamps)

	uploadTimesNanoseconds = make([]int64, len(d.uploadTimesNanoseconds))
	copy(uploadTimesNanoseconds, d.uploadTimesNanoseconds)

	return // Go "named return": returns the two variables declared in the function signature
}

/* -------------------------------------------------------------- */
// Registry
/* -------------------------------------------------------------- */

// Registry is the top-level in-memory store that maps device ID strings to Device objects.
//
// It uses a sync.RWMutex (read/write mutex) rather than a plain mutex. The
// difference: a plain mutex allows only one goroutine at a time (reads block
// reads). An RWMutex allows unlimited concurrent readers, blocking only when
// a write is happening. Since CSV loading is a one-time startup write and
// all runtime traffic is reads, this is a meaningful scalability win.
type Registry struct {
	// devicesByID is the map from a device's string ID to its Device object.
	// Go maps are not safe for concurrent access without a lock - the mutex above
	// is what makes it safe here.
	devicesByID map[string]*Device

	// mu is the read/write mutex protecting devicesByID.
	// RLock/RUnlock for reads; Lock/Unlock for writes.
	mu sync.RWMutex
}

// NewRegistry creates and returns an empty Registry, ready to be populated.
// In Go, constructor functions are conventionally named New<TypeName>.
func NewRegistry() *Registry {
	var registry *Registry = &Registry{
		// make() is required here - a nil map would panic on the first write.
		devicesByID: make(map[string]*Device),
	}
	return registry
}

// LoadFromCSV reads device IDs from a CSV file and registers each one.
//
// The CSV is expected to have a single column with a header row ("device_id")
// followed by one device ID per row. Returns an error if the file cannot be
// opened or its contents cannot be parsed.
func (r *Registry) LoadFromCSV(filePath string) error {
	// os.Open returns a file handle and an error. We check the error before using the handle.
	var csvFile *os.File
	var openErr error
	csvFile, openErr = os.Open(filePath)
	if openErr != nil {
		// fmt.Errorf with %w "wraps" the original error so callers can inspect it.
		return fmt.Errorf("could not open devices file %s: %w", filePath, openErr)
	}
	defer csvFile.Close() // ensure the file is closed when this function returns

	// ReadAll parses the entire file into a [][]string: a slice of rows where
	// each row is a slice of column values as strings.
	var allRows [][]string
	var parseErr error
	allRows, parseErr = csv.NewReader(csvFile).ReadAll()
	if parseErr != nil {
		return fmt.Errorf("could not parse devices file %s: %w", filePath, parseErr)
	}

	// Take the write lock for the duration of the map insertions.
	r.mu.Lock()
	defer r.mu.Unlock()

	// rowIndex is the zero-based position in the file; row is the slice of column values.
	// We use := here because Go's for-range syntax requires it for loop variables.
	for rowIndex, row := range allRows {
		if rowIndex == 0 {
			continue // row 0 is the header ("device_id"), not a real device
		}

		// The device ID is always the first (and only) column value.
		var deviceID string = row[0]
		r.devicesByID[deviceID] = &Device{ID: deviceID}
	}

	return nil
}

// FindByID looks up a device by its ID string.
// Returns (device, true) if the ID is known, or (nil, false) if it is not.
//
// Uses a read lock so many goroutines can look up devices simultaneously
// without blocking each other - only a concurrent write would cause a wait.
func (r *Registry) FindByID(deviceID string) (*Device, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Looking up a key in a Go map returns two values: the value and a boolean
	// indicating whether the key existed. This is idiomatic Go "comma-ok" pattern.
	var foundDevice *Device
	var exists bool
	foundDevice, exists = r.devicesByID[deviceID]

	return foundDevice, exists
}

// Count returns the number of devices currently in the registry.
// Used at startup to log how many devices were loaded.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.devicesByID)
}

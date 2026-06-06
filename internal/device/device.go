// Package device provides the in-memory device registry and per-device
// telemetry storage used by the Fleet Stats service.
package device

import (
	"encoding/csv"
	"fmt"
	"os"
	"sync"
	"time"
)

/* Device */

// Device holds all in-memory state for a single monitored device.
// Its telemetry fields are unexported so callers use the locking methods below.
type Device struct {
	// ID is the unique string identifier for this device, as it appears in devices.csv.
	ID string

	// heartbeatTimestamps is the full ordered list of times this device checked in.
	// One heartbeat is expected per minute; any missing minute means the device was offline.
	heartbeatTimestamps []time.Time

	// uploadTimesNanoseconds stores each video upload duration reported by this device,
	// measured in nanoseconds. These are averaged to compute the avg_upload_time stat.
	uploadTimesNanoseconds []int64

	// mu protects this device's telemetry slices.
	mu sync.Mutex
}

// RecordHeartbeat appends a heartbeat timestamp to this device's history.
// It acquires the device's lock before writing and releases it when done.
func (d *Device) RecordHeartbeat(sentAt time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()

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
// The lock is held only while copying current state.
func (d *Device) Snapshot() (heartbeatTimestamps []time.Time, uploadTimesNanoseconds []int64) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Return copies so callers cannot mutate the device's internal slices.
	heartbeatTimestamps = make([]time.Time, len(d.heartbeatTimestamps))
	copy(heartbeatTimestamps, d.heartbeatTimestamps)

	uploadTimesNanoseconds = make([]int64, len(d.uploadTimesNanoseconds))
	copy(uploadTimesNanoseconds, d.uploadTimesNanoseconds)

	return
}

/* Registry */

// Registry is the top-level in-memory store that maps device ID strings to Device objects.
// It uses an RWMutex because runtime access is lookup-heavy after startup loading.
type Registry struct {
	// devicesByID maps device IDs to shared Device instances.
	devicesByID map[string]*Device

	// mu protects devicesByID.
	mu sync.RWMutex
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	var registry *Registry = &Registry{
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
	var csvFile *os.File
	var openErr error
	csvFile, openErr = os.Open(filePath)
	if openErr != nil {
		return fmt.Errorf("could not open devices file %s: %w", filePath, openErr)
	}
	defer csvFile.Close()

	var allRows [][]string
	var parseErr error
	allRows, parseErr = csv.NewReader(csvFile).ReadAll()
	if parseErr != nil {
		return fmt.Errorf("could not parse devices file %s: %w", filePath, parseErr)
	}

	// Take the write lock for the duration of the map insertions.
	r.mu.Lock()
	defer r.mu.Unlock()

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
func (r *Registry) FindByID(deviceID string) (*Device, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

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

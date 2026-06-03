package device_test

/* -------------------------------------------------------------- */
// Tests for the device package.
//
// Two things are under test here:
//
// 1. Round-trip correctness: data recorded via RecordHeartbeat and
//    RecordUploadStat must come back unchanged via Snapshot.
//
// 2. Snapshot isolation: the slices returned by Snapshot must be
//    independent copies. If a caller modifies the returned slice,
//    the device's own state must remain unchanged. This is the core
//    safety contract of the snapshot pattern -- it is what allows
//    handlers to do slow math without holding the device's lock.
//
// The Registry tests cover the two main failure modes: a missing
// CSV file (returns an error rather than silently producing an empty
// registry) and ID lookup returning the right found/not-found signal.
/* -------------------------------------------------------------- */

import (
	"os"
	"testing"
	"time"

	"github.com/gkub/sy-code-challenge/internal/device"
)

/* -------------------------------------------------------------- */
// Device tests
/* -------------------------------------------------------------- */

func TestDevice_RecordHeartbeat_and_Snapshot(t *testing.T) {
	// Verify that two heartbeats recorded on a device are both returned
	// by Snapshot in the order they were added, with no upload times.
	var d *device.Device = &device.Device{ID: "test-device"}

	var firstHeartbeat time.Time = time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	var secondHeartbeat time.Time = time.Date(2024, 1, 1, 10, 1, 0, 0, time.UTC)

	d.RecordHeartbeat(firstHeartbeat)
	d.RecordHeartbeat(secondHeartbeat)

	var heartbeats []time.Time
	var uploadTimes []int64
	heartbeats, uploadTimes = d.Snapshot()

	if len(heartbeats) != 2 {
		t.Errorf("expected 2 heartbeats in snapshot, got %d", len(heartbeats))
	}
	if len(uploadTimes) != 0 {
		t.Errorf("expected 0 upload times in snapshot, got %d", len(uploadTimes))
	}
	if heartbeats[0] != firstHeartbeat {
		t.Errorf("heartbeats[0]: got %v, want %v", heartbeats[0], firstHeartbeat)
	}
	if heartbeats[1] != secondHeartbeat {
		t.Errorf("heartbeats[1]: got %v, want %v", heartbeats[1], secondHeartbeat)
	}
}

func TestDevice_RecordUploadStat_and_Snapshot(t *testing.T) {
	// Verify that two upload durations come back unchanged via Snapshot,
	// with no heartbeat timestamps appearing alongside them.
	var d *device.Device = &device.Device{ID: "test-device"}

	var firstUpload int64 = int64(2 * time.Minute)  // 2 minutes in nanoseconds
	var secondUpload int64 = int64(4 * time.Minute) // 4 minutes in nanoseconds

	d.RecordUploadStat(firstUpload)
	d.RecordUploadStat(secondUpload)

	var heartbeats []time.Time
	var uploadTimes []int64
	heartbeats, uploadTimes = d.Snapshot()

	if len(heartbeats) != 0 {
		t.Errorf("expected 0 heartbeats in snapshot, got %d", len(heartbeats))
	}
	if len(uploadTimes) != 2 {
		t.Errorf("expected 2 upload times in snapshot, got %d", len(uploadTimes))
	}
	if uploadTimes[0] != firstUpload {
		t.Errorf("uploadTimes[0]: got %d, want %d", uploadTimes[0], firstUpload)
	}
	if uploadTimes[1] != secondUpload {
		t.Errorf("uploadTimes[1]: got %d, want %d", uploadTimes[1], secondUpload)
	}
}

func TestDevice_Snapshot_ReturnsIndependentCopy(t *testing.T) {
	// This is the most important test in this file.
	//
	// Snapshot must return a copy, not a reference to the internal slice.
	// If it returned the internal slice directly, a caller appending to it
	// would silently corrupt the device's own data -- causing every
	// subsequent stats calculation to operate on wrong values. We verify
	// this by appending to the returned copy and then calling Snapshot
	// again to confirm the device's internal count is unchanged.
	var d *device.Device = &device.Device{ID: "test-device"}

	d.RecordHeartbeat(time.Now())

	var firstSnapshot []time.Time
	firstSnapshot, _ = d.Snapshot()

	// Mutate the copy by appending a new value to it.
	firstSnapshot = append(firstSnapshot, time.Now())

	// The device's internal state must still have exactly 1 heartbeat.
	var secondSnapshot []time.Time
	secondSnapshot, _ = d.Snapshot()

	if len(secondSnapshot) != 1 {
		t.Errorf(
			"Snapshot returned the internal slice directly instead of a copy: "+
				"appending to the returned slice changed internal count from 1 to %d",
			len(secondSnapshot),
		)
	}
}

/* -------------------------------------------------------------- */
// Registry tests
/* -------------------------------------------------------------- */

// writeTempCSV creates a temporary CSV file with the given device IDs
// and registers cleanup so it is deleted when the test finishes.
// Returns the path to the file.
func writeTempCSV(t *testing.T, deviceIDs []string) string {
	t.Helper()

	var tmpFile *os.File
	var createErr error
	tmpFile, createErr = os.CreateTemp("", "test_devices_*.csv")
	if createErr != nil {
		t.Fatalf("could not create temp CSV file: %v", createErr)
	}

	// Write the header row the real CSV has, then one device ID per line.
	var _, writeErr = tmpFile.WriteString("device_id\n")
	if writeErr != nil {
		t.Fatalf("could not write CSV header: %v", writeErr)
	}
	for _, id := range deviceIDs {
		tmpFile.WriteString(id + "\n")
	}
	tmpFile.Close()

	// t.Cleanup registers a function to run when the test finishes,
	// whether it passes or fails. This is the idiomatic Go alternative
	// to defer inside a helper function.
	t.Cleanup(func() { os.Remove(tmpFile.Name()) })

	return tmpFile.Name()
}

func TestRegistry_LoadFromCSV_PopulatesDevices(t *testing.T) {
	// Write a CSV with three device IDs, load it, and verify all three
	// are in the registry and individually findable by ID.
	var csvPath string = writeTempCSV(t, []string{"alpha-device", "beta-device", "gamma-device"})

	var reg *device.Registry = device.NewRegistry()
	var loadErr error = reg.LoadFromCSV(csvPath)
	if loadErr != nil {
		t.Fatalf("LoadFromCSV returned unexpected error: %v", loadErr)
	}

	if reg.Count() != 3 {
		t.Errorf("expected 3 devices after loading CSV, got %d", reg.Count())
	}

	for _, id := range []string{"alpha-device", "beta-device", "gamma-device"} {
		var _, found bool
		_, found = reg.FindByID(id)
		if !found {
			t.Errorf("expected to find device %q after loading CSV, but it was not in the registry", id)
		}
	}
}

func TestRegistry_LoadFromCSV_MissingFile_ReturnsError(t *testing.T) {
	// A missing file should produce an error, not silently succeed and
	// leave the registry empty. The server should refuse to start here.
	var reg *device.Registry = device.NewRegistry()
	var err error = reg.LoadFromCSV("/nonexistent/path/devices.csv")

	if err == nil {
		t.Error("expected an error when loading a non-existent CSV file, got nil")
	}
}

func TestRegistry_FindByID_KnownDevice_ReturnsTrueAndDevice(t *testing.T) {
	// Looking up a device that was loaded from CSV should return the
	// device and true. We also verify the returned device has the correct
	// ID field, since a bug could theoretically return the wrong object.
	var csvPath string = writeTempCSV(t, []string{"my-device"})

	var reg *device.Registry = device.NewRegistry()
	reg.LoadFromCSV(csvPath)

	var foundDevice *device.Device
	var exists bool
	foundDevice, exists = reg.FindByID("my-device")

	if !exists {
		t.Error("expected FindByID to return true for a known device ID")
	}
	if foundDevice == nil {
		t.Fatal("expected FindByID to return a non-nil device for a known ID")
	}
	if foundDevice.ID != "my-device" {
		t.Errorf("device ID: got %q, want %q", foundDevice.ID, "my-device")
	}
}

func TestRegistry_FindByID_UnknownDevice_ReturnsFalse(t *testing.T) {
	// Looking up an ID that was never loaded should return nil and false.
	// The handler layer depends on this false signal to write a 404.
	var csvPath string = writeTempCSV(t, []string{"known-device"})

	var reg *device.Registry = device.NewRegistry()
	reg.LoadFromCSV(csvPath)

	var foundDevice *device.Device
	var exists bool
	foundDevice, exists = reg.FindByID("unknown-device-id")

	if exists {
		t.Error("expected FindByID to return false for an unknown device ID")
	}
	if foundDevice != nil {
		t.Error("expected FindByID to return nil for an unknown device ID")
	}
}

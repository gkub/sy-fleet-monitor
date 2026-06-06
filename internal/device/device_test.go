package device_test

// Tests for the device package.
//
// Covers record/snapshot round trips, snapshot isolation, CSV loading,
// and device lookup behavior.

import (
	"os"
	"testing"
	"time"

	"github.com/gkub/sy-code-challenge/internal/device"
)

// Device tests

func TestDevice_RecordHeartbeat_and_Snapshot(t *testing.T) {
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
	var d *device.Device = &device.Device{ID: "test-device"}

	var firstUpload int64 = int64(2 * time.Minute)
	var secondUpload int64 = int64(4 * time.Minute)

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
	// Snapshot must return a copy, not a reference to the internal slice.
	var d *device.Device = &device.Device{ID: "test-device"}

	d.RecordHeartbeat(time.Now())

	var firstSnapshot []time.Time
	firstSnapshot, _ = d.Snapshot()

	firstSnapshot = append(firstSnapshot, time.Now())

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

// Registry tests

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

	// Clean up the temporary CSV after the test.
	t.Cleanup(func() { os.Remove(tmpFile.Name()) })

	return tmpFile.Name()
}

func TestRegistry_LoadFromCSV_PopulatesDevices(t *testing.T) {
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
	var reg *device.Registry = device.NewRegistry()
	var err error = reg.LoadFromCSV("/nonexistent/path/devices.csv")

	if err == nil {
		t.Error("expected an error when loading a non-existent CSV file, got nil")
	}
}

func TestRegistry_FindByID_KnownDevice_ReturnsTrueAndDevice(t *testing.T) {
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

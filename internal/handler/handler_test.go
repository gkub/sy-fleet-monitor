package handler_test

// Tests for the handler package.
//
// Verifies status codes, JSON response shapes, and error handling by
// calling handlers directly with httptest.
import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gkub/sy-code-challenge/internal/device"
	"github.com/gkub/sy-code-challenge/internal/handler"
)

// Local response types
//
// The response structs in handler.go are unexported, so we re-declare them
// here just for decoding.

// statsResponseBody matches the JSON returned by GET .../stats.
type statsResponseBody struct {
	Uptime        float64 `json:"uptime"`
	AvgUploadTime string  `json:"avg_upload_time"`
}

// errorResponseBody matches the JSON returned with 4xx/5xx responses.
// The OpenAPI contract requires the key to be "msg", not "message" or "error".
type errorResponseBody struct {
	Message string `json:"msg"`
}

// Test helpers

// setupRegistry writes a temporary CSV with the given device IDs,
// loads it into a fresh Registry, and registers cleanup so the file
// is deleted after the test. Using a real CSV exercises the same
// LoadFromCSV path that runs in production, rather than a mock.
func setupRegistry(t *testing.T, deviceIDs []string) *device.Registry {
	t.Helper()

	var tmpFile *os.File
	var err error
	tmpFile, err = os.CreateTemp("", "test_devices_*.csv")
	if err != nil {
		t.Fatalf("setupRegistry: could not create temp file: %v", err)
	}
	t.Cleanup(func() { os.Remove(tmpFile.Name()) })

	tmpFile.WriteString("device_id\n")
	for _, id := range deviceIDs {
		tmpFile.WriteString(id + "\n")
	}
	tmpFile.Close()

	var reg *device.Registry = device.NewRegistry()
	if err = reg.LoadFromCSV(tmpFile.Name()); err != nil {
		t.Fatalf("setupRegistry: LoadFromCSV failed: %v", err)
	}
	return reg
}

// postRequest builds a fake POST request with a JSON body and injects
// deviceID as the {device_id} path value.
func postRequest(path, jsonBody, deviceID string) *http.Request {
	var req *http.Request = httptest.NewRequest(
		http.MethodPost,
		path,
		strings.NewReader(jsonBody),
	)
	req.SetPathValue("device_id", deviceID)
	return req
}

// getRequest builds a fake GET request with no body.
func getRequest(path, deviceID string) *http.Request {
	var req *http.Request = httptest.NewRequest(http.MethodGet, path, nil)
	req.SetPathValue("device_id", deviceID)
	return req
}

// RecordHeartbeat tests

func TestRecordHeartbeat_KnownDevice_Returns204(t *testing.T) {
	var reg *device.Registry = setupRegistry(t, []string{"device-001"})
	var handlerFunc http.HandlerFunc = handler.RecordHeartbeat(reg)

	var req *http.Request = postRequest(
		"/api/v1/devices/device-001/heartbeat",
		`{"sent_at":"2024-01-01T10:00:00Z"}`,
		"device-001",
	)
	var recorder *httptest.ResponseRecorder = httptest.NewRecorder()

	handlerFunc(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Errorf("expected status 204, got %d", recorder.Code)
	}
}

func TestRecordHeartbeat_UnknownDevice_Returns404WithMsg(t *testing.T) {
	// Unknown devices return the required JSON error shape.
	var reg *device.Registry = setupRegistry(t, []string{"device-001"})
	var handlerFunc http.HandlerFunc = handler.RecordHeartbeat(reg)

	var req *http.Request = postRequest(
		"/api/v1/devices/does-not-exist/heartbeat",
		`{"sent_at":"2024-01-01T10:00:00Z"}`,
		"does-not-exist",
	)
	var recorder *httptest.ResponseRecorder = httptest.NewRecorder()

	handlerFunc(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", recorder.Code)
	}

	var responseBody errorResponseBody
	json.NewDecoder(recorder.Body).Decode(&responseBody)
	if responseBody.Message == "" {
		t.Error("expected a non-empty 'msg' field in the 404 response body")
	}
}

func TestRecordHeartbeat_InvalidJSON_Returns400(t *testing.T) {
	var reg *device.Registry = setupRegistry(t, []string{"device-001"})
	var handlerFunc http.HandlerFunc = handler.RecordHeartbeat(reg)

	var req *http.Request = postRequest(
		"/api/v1/devices/device-001/heartbeat",
		`this is not json`,
		"device-001",
	)
	var recorder *httptest.ResponseRecorder = httptest.NewRecorder()

	handlerFunc(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", recorder.Code)
	}
}

// RecordUploadStats tests

func TestRecordUploadStats_KnownDevice_Returns204(t *testing.T) {
	var reg *device.Registry = setupRegistry(t, []string{"device-001"})
	var handlerFunc http.HandlerFunc = handler.RecordUploadStats(reg)

	var req *http.Request = postRequest(
		"/api/v1/devices/device-001/stats",
		`{"sent_at":"2024-01-01T10:00:00Z","upload_time":197331667813}`,
		"device-001",
	)
	var recorder *httptest.ResponseRecorder = httptest.NewRecorder()

	handlerFunc(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Errorf("expected status 204, got %d", recorder.Code)
	}
}

func TestRecordUploadStats_UnknownDevice_Returns404(t *testing.T) {
	var reg *device.Registry = setupRegistry(t, []string{"device-001"})
	var handlerFunc http.HandlerFunc = handler.RecordUploadStats(reg)

	var req *http.Request = postRequest(
		"/api/v1/devices/ghost-device/stats",
		`{"sent_at":"2024-01-01T10:00:00Z","upload_time":1000}`,
		"ghost-device",
	)
	var recorder *httptest.ResponseRecorder = httptest.NewRecorder()

	handlerFunc(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", recorder.Code)
	}
}

func TestRecordUploadStats_WrongType_Returns400(t *testing.T) {
	// upload_time must be a JSON integer.
	var reg *device.Registry = setupRegistry(t, []string{"device-001"})
	var handlerFunc http.HandlerFunc = handler.RecordUploadStats(reg)

	var req *http.Request = postRequest(
		"/api/v1/devices/device-001/stats",
		`{"sent_at":"2024-01-01T10:00:00Z","upload_time":"should_be_a_number"}`,
		"device-001",
	)
	var recorder *httptest.ResponseRecorder = httptest.NewRecorder()

	handlerFunc(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", recorder.Code)
	}
}

// GetDeviceStats tests

func TestGetDeviceStats_UnknownDevice_Returns404(t *testing.T) {
	var reg *device.Registry = setupRegistry(t, []string{"device-001"})
	var handlerFunc http.HandlerFunc = handler.GetDeviceStats(reg)

	var req *http.Request = getRequest("/api/v1/devices/ghost-device/stats", "ghost-device")
	var recorder *httptest.ResponseRecorder = httptest.NewRecorder()

	handlerFunc(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", recorder.Code)
	}
}

func TestGetDeviceStats_NoDataYet_ReturnsZeroValues(t *testing.T) {
	// Known devices with no telemetry return zero-value stats.
	var reg *device.Registry = setupRegistry(t, []string{"device-001"})
	var handlerFunc http.HandlerFunc = handler.GetDeviceStats(reg)

	var req *http.Request = getRequest("/api/v1/devices/device-001/stats", "device-001")
	var recorder *httptest.ResponseRecorder = httptest.NewRecorder()

	handlerFunc(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}

	var responseBody statsResponseBody
	json.NewDecoder(recorder.Body).Decode(&responseBody)

	if responseBody.Uptime != 0 {
		t.Errorf("uptime: got %.5f, want 0", responseBody.Uptime)
	}
	if responseBody.AvgUploadTime != "0s" {
		t.Errorf("avg_upload_time: got %q, want \"0s\"", responseBody.AvgUploadTime)
	}
}

func TestGetDeviceStats_WithData_ReturnsCorrectStats(t *testing.T) {
	// Drive all three endpoints together and verify the computed output.
	//
	// Setup:
	//   Two heartbeats at 10:00 and 10:04 (minutes 0 and 4, gap in between).
	//   Uptime formula: 2 covered minutes / 4-minute span = 50%.
	//
	//   Two upload times: 1 minute and 3 minutes.
	//   Average: (1 + 3) / 2 = 2 minutes, formatted as "2m0s".
	var reg *device.Registry = setupRegistry(t, []string{"device-001"})

	var heartbeatHandler http.HandlerFunc = handler.RecordHeartbeat(reg)
	for _, sentAt := range []string{"2024-01-01T10:00:00Z", "2024-01-01T10:04:00Z"} {
		var req *http.Request = postRequest(
			"/api/v1/devices/device-001/heartbeat",
			fmt.Sprintf(`{"sent_at":"%s"}`, sentAt),
			"device-001",
		)
		heartbeatHandler(httptest.NewRecorder(), req)
	}

	var uploadHandler http.HandlerFunc = handler.RecordUploadStats(reg)
	for _, uploadNs := range []int64{int64(time.Minute), int64(3 * time.Minute)} {
		var req *http.Request = postRequest(
			"/api/v1/devices/device-001/stats",
			fmt.Sprintf(`{"sent_at":"2024-01-01T10:00:00Z","upload_time":%d}`, uploadNs),
			"device-001",
		)
		uploadHandler(httptest.NewRecorder(), req)
	}

	var getHandler http.HandlerFunc = handler.GetDeviceStats(reg)
	var req *http.Request = getRequest("/api/v1/devices/device-001/stats", "device-001")
	var recorder *httptest.ResponseRecorder = httptest.NewRecorder()
	getHandler(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}

	var responseBody statsResponseBody
	json.NewDecoder(recorder.Body).Decode(&responseBody)

	if responseBody.Uptime != 50.0 {
		t.Errorf("uptime: got %.5f, want 50.0", responseBody.Uptime)
	}
	if responseBody.AvgUploadTime != "2m0s" {
		t.Errorf("avg_upload_time: got %q, want \"2m0s\"", responseBody.AvgUploadTime)
	}
}

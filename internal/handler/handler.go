// The handler package contains the HTTP handler functions for the Fleet Stats API.
//
// Each exported function (RecordHeartbeat, RecordUploadStats, GetDeviceStats)
// takes a *device.Registry and returns an http.HandlerFunc. Returning a function
// rather than being one directly is the "closure" pattern: the returned handler
// "closes over" the registry argument, giving it access to device data on every
// request without relying on global variables.
//
// This file also owns all JSON request/response struct definitions, because they
// are HTTP-layer details with no meaning outside of this package.
package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gkub/sy-code-challenge/internal/device"
	"github.com/gkub/sy-code-challenge/internal/metrics"
)

/* -------------------------------------------------------------- */
// Request and response types
/* -------------------------------------------------------------- */

// Unexported types (lowercase first letter) are private to this package.
// The `json:"field_name"` struct tags tell encoding/json which key to use
// when reading from or writing to JSON. Without a tag, it defaults to the
// exact field name, which wouldn't match the snake_case the API contract uses.

// heartbeatRequest is the JSON body the device sends to POST .../heartbeat.
type heartbeatRequest struct {
	// SentAt is the UTC timestamp when the device generated this heartbeat.
	SentAt time.Time `json:"sent_at"`
}

// uploadStatsRequest is the JSON body the device sends to POST .../stats.
type uploadStatsRequest struct {
	// SentAt is the UTC timestamp when the device sent this report.
	SentAt time.Time `json:"sent_at"`

	// UploadTime is the number of nanoseconds the last video upload took.
	UploadTime int64 `json:"upload_time"`
}

// deviceStatsResponse is the JSON body returned by GET .../stats.
type deviceStatsResponse struct {
	// Uptime is the device's availability percentage (e.g. 98.5).
	Uptime float64 `json:"uptime"`

	// AvgUploadTime is the mean upload duration as a human-readable string
	// (e.g. "5m10s"). The format comes from time.Duration.String().
	AvgUploadTime string `json:"avg_upload_time"`
}

// errorResponse is the JSON body sent with any 4xx or 5xx response.
// The OpenAPI contract requires the field name to be "msg".
type errorResponse struct {
	Message string `json:"msg"`
}

/* -------------------------------------------------------------- */
// Handlers
/* -------------------------------------------------------------- */

// RecordHeartbeat returns the handler for POST /api/v1/devices/{device_id}/heartbeat.
//
// It reads the sent_at timestamp from the JSON body and appends it to the
// target device's heartbeat history. Responds with 204 No Content on success -
// meaning the request was accepted but there is no response body to return.
func RecordHeartbeat(deviceRegistry *device.Registry) http.HandlerFunc {
	return func(responseWriter http.ResponseWriter, httpRequest *http.Request) {
		// Look up the device identified by {device_id} in the URL.
		// lookupDevice writes a 404 and returns false if the device is unknown.
		var targetDevice *device.Device
		var deviceFound bool
		targetDevice, deviceFound = lookupDevice(deviceRegistry, responseWriter, httpRequest)
		if !deviceFound {
			return // response already written by lookupDevice
		}

		// Decode the JSON request body into our heartbeatRequest struct.
		// &requestBody passes a pointer so Decode can fill in its fields.
		var requestBody heartbeatRequest
		var decodeErr error = json.NewDecoder(httpRequest.Body).Decode(&requestBody)
		if decodeErr != nil {
			writeError(responseWriter, http.StatusBadRequest, "invalid request body: expected JSON with a sent_at timestamp")
			return
		}

		// Persist the heartbeat. RecordHeartbeat handles its own locking.
		targetDevice.RecordHeartbeat(requestBody.SentAt)

		// 204 No Content: success, but we have nothing to put in the response body.
		responseWriter.WriteHeader(http.StatusNoContent)
	}
}

// RecordUploadStats returns the handler for POST /api/v1/devices/{device_id}/stats.
//
// It reads the upload_time (nanoseconds) from the JSON body and appends it to
// the target device's upload-time history. Responds 204 No Content on success.
func RecordUploadStats(deviceRegistry *device.Registry) http.HandlerFunc {
	return func(responseWriter http.ResponseWriter, httpRequest *http.Request) {
		var targetDevice *device.Device
		var deviceFound bool
		targetDevice, deviceFound = lookupDevice(deviceRegistry, responseWriter, httpRequest)
		if !deviceFound {
			return
		}

		var requestBody uploadStatsRequest
		var decodeErr error = json.NewDecoder(httpRequest.Body).Decode(&requestBody)
		if decodeErr != nil {
			writeError(responseWriter, http.StatusBadRequest, "invalid request body: expected JSON with sent_at and upload_time fields")
			return
		}

		// Persist the upload duration. RecordUploadStat handles its own locking.
		targetDevice.RecordUploadStat(requestBody.UploadTime)

		responseWriter.WriteHeader(http.StatusNoContent)
	}
}

// GetDeviceStats returns the handler for GET /api/v1/devices/{device_id}/stats.
//
// It retrieves a snapshot of the device's telemetry history, computes uptime
// and average upload duration, and returns them as JSON.
func GetDeviceStats(deviceRegistry *device.Registry) http.HandlerFunc {
	return func(responseWriter http.ResponseWriter, httpRequest *http.Request) {
		var targetDevice *device.Device
		var deviceFound bool
		targetDevice, deviceFound = lookupDevice(deviceRegistry, responseWriter, httpRequest)
		if !deviceFound {
			return
		}

		// Snapshot returns independent copies of the device's data.
		// We do all computation on the copies so the device's mutex is released
		// as quickly as possible, keeping other goroutines from waiting.
		var heartbeatTimestamps []time.Time
		var uploadTimesNanoseconds []int64
		heartbeatTimestamps, uploadTimesNanoseconds = targetDevice.Snapshot()

		// Delegate all number-crunching to the metrics package.
		var uptimePercent float64 = metrics.CalculateUptime(heartbeatTimestamps)
		var averageUploadDuration time.Duration = metrics.CalculateAverageUploadDuration(uploadTimesNanoseconds)

		// Build the response struct. time.Duration.String() produces the
		// human-readable format ("5m10s") required by the OpenAPI contract.
		var responseBody deviceStatsResponse = deviceStatsResponse{
			Uptime:        uptimePercent,
			AvgUploadTime: averageUploadDuration.String(),
		}

		writeJSON(responseWriter, http.StatusOK, responseBody)
	}
}

/* -------------------------------------------------------------- */
// Shared helpers
/* -------------------------------------------------------------- */

// lookupDevice extracts {device_id} from the URL, looks it up in the registry,
// and writes a JSON 404 if the device is not found.
//
// The bool return tells callers whether the lookup succeeded. If it returns
// false, the error response has already been written - callers should just return.
func lookupDevice(
	deviceRegistry *device.Registry,
	responseWriter http.ResponseWriter,
	httpRequest *http.Request,
) (*device.Device, bool) {
	// PathValue extracts the named segment from the route pattern.
	// For a route registered as ".../devices/{device_id}/...", this returns
	// whatever string appeared in the {device_id} position of the actual URL.
	var deviceID string = httpRequest.PathValue("device_id")

	var foundDevice *device.Device
	var exists bool
	foundDevice, exists = deviceRegistry.FindByID(deviceID)

	if !exists {
		writeError(responseWriter, http.StatusNotFound, "device not found: "+deviceID)
	}
	return foundDevice, exists
}

// writeJSON serialises responseBody to JSON and writes it to the response with
// the given HTTP status code. It sets Content-Type before WriteHeader because
// Go's http package ignores headers set after the status code is written.
func writeJSON(responseWriter http.ResponseWriter, statusCode int, responseBody any) {
	responseWriter.Header().Set("Content-Type", "application/json")
	responseWriter.WriteHeader(statusCode)
	json.NewEncoder(responseWriter).Encode(responseBody)
}

// writeError writes a JSON error response in the format {"msg": "..."} that
// the OpenAPI contract requires for all error responses.
func writeError(responseWriter http.ResponseWriter, statusCode int, message string) {
	var errorBody errorResponse = errorResponse{Message: message}
	writeJSON(responseWriter, statusCode, errorBody)
}

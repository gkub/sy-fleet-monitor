// Package handler exposes HTTP handlers for the Fleet Stats API.
// It owns request/response JSON types and delegates storage and metric work to
// the device and metrics packages.
package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gkub/sy-code-challenge/internal/device"
	"github.com/gkub/sy-code-challenge/internal/metrics"
)

/* Request and response types */

// JSON field names follow the OpenAPI contract.

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

/* Handlers */

// RecordHeartbeat returns the handler for POST /api/v1/devices/{device_id}/heartbeat.
//
// It reads the sent_at timestamp from the JSON body and appends it to the
// target device's heartbeat history. Responds with 204 No Content on success.
func RecordHeartbeat(deviceRegistry *device.Registry) http.HandlerFunc {
	return func(responseWriter http.ResponseWriter, httpRequest *http.Request) {
		var targetDevice *device.Device
		var deviceFound bool
		targetDevice, deviceFound = lookupDevice(deviceRegistry, responseWriter, httpRequest)
		if !deviceFound {
			return // response already written by lookupDevice
		}

		var requestBody heartbeatRequest
		var decodeErr error = json.NewDecoder(httpRequest.Body).Decode(&requestBody)
		if decodeErr != nil {
			writeError(responseWriter, http.StatusBadRequest, "invalid request body: expected JSON with a sent_at timestamp")
			return
		}

		targetDevice.RecordHeartbeat(requestBody.SentAt)

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

		// Snapshot returns copies so metric calculations do not hold the device lock.
		var heartbeatTimestamps []time.Time
		var uploadTimesNanoseconds []int64
		heartbeatTimestamps, uploadTimesNanoseconds = targetDevice.Snapshot()

		var uptimePercent float64 = metrics.CalculateUptime(heartbeatTimestamps)
		var averageUploadDuration time.Duration = metrics.CalculateAverageUploadDuration(uploadTimesNanoseconds)

		// The API contract expects avg_upload_time as a duration string.
		var responseBody deviceStatsResponse = deviceStatsResponse{
			Uptime:        uptimePercent,
			AvgUploadTime: averageUploadDuration.String(),
		}

		writeJSON(responseWriter, http.StatusOK, responseBody)
	}
}

/* Shared helpers */

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
	// Read device_id from the matched route.
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
// the given HTTP status code.
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

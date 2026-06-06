// Command fleetstats starts the Fleet Statistics HTTP server.
//
// It loads a list of known devices from a CSV file on startup, then listens
// for telemetry from those devices on three endpoints:
//
//	POST /api/v1/devices/{device_id}/heartbeat  - device reports that it is alive
//	POST /api/v1/devices/{device_id}/stats      - device reports a video upload duration
//	GET  /api/v1/devices/{device_id}/stats      - query computed uptime and avg upload time
//
// Usage:
//
//	go run ./cmd/fleetstats -devices path/to/devices.csv
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/gkub/sy-code-challenge/internal/device"
	"github.com/gkub/sy-code-challenge/internal/handler"
)

/* Configuration */

// serverPort is fixed by the OpenAPI contract and expected by the simulator.
const serverPort int = 6733

// apiBasePath is the URL prefix shared by all API routes.
const apiBasePath string = "/api/v1"

/* Entry point */

func main() {
	// Read the known-device CSV path from the -devices flag.
	var deviceCSVPath *string = flag.String("devices", "devices.csv", "path to the CSV file containing device IDs")

	flag.Parse()

	// The shared registry is loaded once at startup and passed to all handlers.
	var deviceRegistry *device.Registry = device.NewRegistry()

	// Load known devices before accepting traffic.
	var loadErr error = deviceRegistry.LoadFromCSV(*deviceCSVPath)
	if loadErr != nil {
		// A registry load failure is fatal; unknown devices cannot be tracked.
		log.Fatalf("failed to load devices from %q: %v", *deviceCSVPath, loadErr)
	}

	log.Printf("loaded %d devices from %q", deviceRegistry.Count(), *deviceCSVPath)

	// Register method-aware routes with a named device_id path segment.
	var requestRouter *http.ServeMux = http.NewServeMux()

	requestRouter.HandleFunc(
		fmt.Sprintf("POST %s/devices/{device_id}/heartbeat", apiBasePath),
		handler.RecordHeartbeat(deviceRegistry),
	)
	requestRouter.HandleFunc(
		fmt.Sprintf("POST %s/devices/{device_id}/stats", apiBasePath),
		handler.RecordUploadStats(deviceRegistry),
	)
	requestRouter.HandleFunc(
		fmt.Sprintf("GET %s/devices/{device_id}/stats", apiBasePath),
		handler.GetDeviceStats(deviceRegistry),
	)

	// Bind to all interfaces on the contract port.
	var listenAddress string = fmt.Sprintf(":%d", serverPort)

	log.Printf("fleet stats server listening on %s", listenAddress)

	var serverErr error = http.ListenAndServe(listenAddress, requestRouter)
	log.Fatalf("server stopped unexpectedly: %v", serverErr)
}

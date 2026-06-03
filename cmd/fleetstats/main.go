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

/* -------------------------------------------------------------- */
// Configuration
/* -------------------------------------------------------------- */

// serverPort is the TCP port this server listens on.
// Specified in the OpenAPI contract - the device simulator expects this exact port.
const serverPort int = 6733

// apiBasePath is the URL prefix shared by all routes.
// All paths in the OpenAPI contract are relative to this base.
const apiBasePath string = "/api/v1"

/* -------------------------------------------------------------- */
// Entry point
/* -------------------------------------------------------------- */

func main() {
	// deviceCSVPath holds the file path to the CSV listing all known device IDs.
	// It is read from the -devices command-line flag so it can vary by environment
	// without changing the code. flag.String registers the flag and returns a pointer.
	var deviceCSVPath *string = flag.String("devices", "devices.csv", "path to the CSV file containing device IDs")

	// flag.Parse reads os.Args and fills in the registered flag variables above.
	flag.Parse()

	// deviceRegistry is the shared in-memory store for all devices and their
	// telemetry. It is created once here and passed into each HTTP handler.
	// Using a single shared registry (rather than global state) makes the
	// data flow explicit and keeps the code testable.
	var deviceRegistry *device.Registry = device.NewRegistry()

	// Load all device IDs from the CSV before starting the server.
	// The pointer dereference *deviceCSVPath gets the actual string value
	// from the pointer that flag.String returned.
	var loadErr error = deviceRegistry.LoadFromCSV(*deviceCSVPath)
	if loadErr != nil {
		// log.Fatalf prints the message and then calls os.Exit(1), stopping the program.
		// There is no point starting the server if we have no devices to track.
		log.Fatalf("failed to load devices from %q: %v", *deviceCSVPath, loadErr)
	}

	log.Printf("loaded %d devices from %q", deviceRegistry.Count(), *deviceCSVPath)

	// requestRouter is Go's standard library HTTP multiplexer (router).
	// It matches incoming request URLs to the correct handler function.
	// As of Go 1.22, patterns can include the HTTP method and named path
	// segments like {device_id}, which the handler reads via r.PathValue().
	var requestRouter *http.ServeMux = http.NewServeMux()

	// Register all three routes. fmt.Sprintf builds the full pattern by
	// prepending the shared apiBasePath to each endpoint path.
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

	// listenAddress is the host:port string the HTTP server binds to.
	// An empty host string (":6733") means "listen on all network interfaces."
	var listenAddress string = fmt.Sprintf(":%d", serverPort)

	log.Printf("fleet stats server listening on %s", listenAddress)

	// ListenAndServe blocks forever, handling incoming requests.
	// It only returns if the server fails to start or encounters a fatal error.
	var serverErr error = http.ListenAndServe(listenAddress, requestRouter)
	log.Fatalf("server stopped unexpectedly: %v", serverErr)
}

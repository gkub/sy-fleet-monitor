// The metrics package contains pure calculation functions for device telemetry.
//
// Functions here accept plain data (slices of timestamps, slices of integers)
// and return computed results. There is no HTTP, no device registry, and no
// global state in this package. That isolation makes these functions trivial
// to unit test: call them with hand-crafted input, assert on the output.
package metrics

import "time"

/* -------------------------------------------------------------- */
// Uptime
/* -------------------------------------------------------------- */

// CalculateUptime returns the device's uptime as a percentage between 0 and 100.
//
// A device is expected to send exactly one heartbeat per minute. Uptime is the
// fraction of one-minute windows - from the first heartbeat to the last - that
// contained at least one heartbeat, expressed as a percentage:
//
//	uptime = (minutesWithAtLeastOneHeartbeat / totalMinutesInSpan) * 100
//
// Example: heartbeats arrive at 10:00, 10:01, 10:03 (10:02 is missing).
//
//	minutesWithHeartbeat = 3
//	totalMinutesInSpan   = 4  (10:00, 10:01, 10:02, 10:03 inclusive)
//	uptime               = (3 / 4) * 100 = 75.0
//
// Returns 0 if fewer than two heartbeats have been received - not enough data
// to define a meaningful time span.
func CalculateUptime(heartbeatTimestamps []time.Time) float64 {
	// We need at least two timestamps to calculate a span.
	if len(heartbeatTimestamps) < 2 {
		return 0
	}

	// minuteBucketSet is used as a set of distinct one-minute windows that had
	// a heartbeat. A Go map with struct{} values is the idiomatic way to build
	// a set: the struct{} value costs zero bytes, so we pay only for the keys.
	var minuteBucketSet map[time.Time]struct{} = make(map[time.Time]struct{})

	// earliestHeartbeat and latestHeartbeat track the boundaries of the total
	// time span. We scan every timestamp to find them.
	var earliestHeartbeat time.Time
	var latestHeartbeat time.Time

	for _, heartbeatTime := range heartbeatTimestamps {
		// Truncate rounds the timestamp down to the start of its minute,
		// e.g. 10:04:37 UTC → 10:04:00 UTC. This groups all heartbeats
		// that arrived within the same minute into the same bucket.
		var minuteBucket time.Time = heartbeatTime.UTC().Truncate(time.Minute)

		// Assigning struct{}{} marks this minute as "seen".
		// Re-assigning the same key is a no-op, which automatically deduplicates.
		minuteBucketSet[minuteBucket] = struct{}{}

		// Update the earliest/latest boundaries.
		// IsZero() checks whether a time.Time has its zero value (not yet set).
		if earliestHeartbeat.IsZero() || heartbeatTime.Before(earliestHeartbeat) {
			earliestHeartbeat = heartbeatTime
		}
		if heartbeatTime.After(latestHeartbeat) {
			latestHeartbeat = heartbeatTime
		}
	}

	// Align the boundary times to minute boundaries so the span count is
	// consistent with how we bucketed individual heartbeats above.
	var firstMinuteBucket time.Time = earliestHeartbeat.UTC().Truncate(time.Minute)
	var lastMinuteBucket time.Time = latestHeartbeat.UTC().Truncate(time.Minute)

	// Sub() returns a time.Duration. .Minutes() converts it to a float64 count of minutes.
	// The simulator's heartbeats span exactly N minutes from first to last (e.g. 480 minutes
	// for an 8-hour window), so we use the raw duration without adding 1.
	var totalMinutesInSpan float64 = lastMinuteBucket.Sub(firstMinuteBucket).Minutes()

	// Guard against the degenerate case where all heartbeats fall in the same minute,
	// which would produce a zero denominator. Treat it as 100% - we observed the device
	// the whole time and it was always up.
	if totalMinutesInSpan == 0 {
		return 100.0
	}

	// float64() converts the integer map length to a floating-point number
	// so the division below produces a decimal rather than truncating to an integer.
	var minutesWithHeartbeat float64 = float64(len(minuteBucketSet))

	return (minutesWithHeartbeat / totalMinutesInSpan) * 100
}

/* -------------------------------------------------------------- */
// Average upload duration
/* -------------------------------------------------------------- */

// CalculateAverageUploadDuration returns the arithmetic mean of a list of
// video upload durations, expressed as a time.Duration.
//
// uploadTimesNanoseconds contains raw nanosecond counts as reported by devices.
// The return type is time.Duration - which is just an int64 counting nanoseconds
// under the hood - because calling .String() on it produces the formatted
// duration string the API contract requires (e.g. "5m10s", "2h3m4.5s").
//
// Returns 0 if the input slice is empty.
func CalculateAverageUploadDuration(uploadTimesNanoseconds []int64) time.Duration {
	if len(uploadTimesNanoseconds) == 0 {
		return 0
	}

	// totalNanoseconds accumulates the sum of all upload durations.
	var totalNanoseconds int64 = 0
	for _, singleUploadTimeNanoseconds := range uploadTimesNanoseconds {
		totalNanoseconds = totalNanoseconds + singleUploadTimeNanoseconds
	}

	// Integer division gives us the mean nanosecond count.
	var averageNanoseconds int64 = totalNanoseconds / int64(len(uploadTimesNanoseconds))

	// time.Duration(x) converts a plain int64 nanosecond count into a Duration value.
	// This unlocks .String(), which formats it as "5m10s", "72h3m0.5s", etc. -
	// exactly the format required by the OpenAPI contract.
	var averageDuration time.Duration = time.Duration(averageNanoseconds)

	return averageDuration
}

// Package metrics contains telemetry calculation functions for device stats.
package metrics

import "time"

/* Uptime */

// CalculateUptime returns the device's uptime as a percentage between 0 and 100.
//
// A device is expected to send exactly one heartbeat per minute. Uptime is the
// fraction of one-minute windows - from the first heartbeat to the last - that
// contained at least one heartbeat, expressed as a percentage:
//
//	uptime = (minutesWithAtLeastOneHeartbeat / totalMinutesInSpan) * 100
//
// Returns 0 if fewer than two heartbeats have been received - not enough data
// to define a meaningful time span.
func CalculateUptime(heartbeatTimestamps []time.Time) float64 {
	if len(heartbeatTimestamps) < 2 {
		return 0
	}

	// Track distinct one-minute windows that had at least one heartbeat.
	var minuteBucketSet map[time.Time]struct{} = make(map[time.Time]struct{})

	var earliestHeartbeat time.Time
	var latestHeartbeat time.Time

	for _, heartbeatTime := range heartbeatTimestamps {
		// Group all heartbeats in the same minute into one bucket.
		var minuteBucket time.Time = heartbeatTime.UTC().Truncate(time.Minute)
		minuteBucketSet[minuteBucket] = struct{}{}

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

	// The simulator expects the raw first-to-last minute span, without adding
	// an extra inclusive endpoint minute.
	var totalMinutesInSpan float64 = lastMinuteBucket.Sub(firstMinuteBucket).Minutes()

	// Guard against the degenerate case where all heartbeats fall in the same minute.
	if totalMinutesInSpan == 0 {
		return 100.0
	}

	var minutesWithHeartbeat float64 = float64(len(minuteBucketSet))

	return (minutesWithHeartbeat / totalMinutesInSpan) * 100
}

/* Average upload duration */

// CalculateAverageUploadDuration returns the arithmetic mean of a list of
// video upload durations, expressed as a time.Duration.
//
// uploadTimesNanoseconds contains raw nanosecond counts as reported by devices.
// The return value is formatted by the handler for the API response.
//
// Returns 0 if the input slice is empty.
func CalculateAverageUploadDuration(uploadTimesNanoseconds []int64) time.Duration {
	if len(uploadTimesNanoseconds) == 0 {
		return 0
	}

	var totalNanoseconds int64 = 0
	for _, singleUploadTimeNanoseconds := range uploadTimesNanoseconds {
		totalNanoseconds = totalNanoseconds + singleUploadTimeNanoseconds
	}

	var averageNanoseconds int64 = totalNanoseconds / int64(len(uploadTimesNanoseconds))

	var averageDuration time.Duration = time.Duration(averageNanoseconds)

	return averageDuration
}

package metrics_test

/* -------------------------------------------------------------- */
// Tests for the metrics package.
//
// Because the functions here are pure (same input always produces
// the same output, no side effects), tests are simple: build some
// input, call the function, check the output. No HTTP server, no
// registry, no CSV file needed.
//
// We use table-driven tests: a slice of named test cases, each
// with inputs and expected output, all run through the same
// assertion logic. This is idiomatic Go and makes it easy to add
// new cases without duplicating boilerplate.
/* -------------------------------------------------------------- */

import (
	"math"
	"testing"
	"time"

	"github.com/gkub/sy-code-challenge/internal/metrics"
)

// baseTime is a fixed reference point used to build test timestamps.
// A fixed time makes tests deterministic -- they don't depend on
// when you run them.
var baseTime = time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)

// minutesAfter returns a timestamp exactly n minutes after baseTime.
// This helper keeps test data readable: minutesAfter(4) is clearer
// than writing time.Date(2024, 1, 1, 10, 4, 0, 0, time.UTC) everywhere.
func minutesAfter(n int) time.Time {
	return baseTime.Add(time.Duration(n) * time.Minute)
}

// floatTolerance is the maximum acceptable difference between two
// float64 values when checking for equality. Floating-point division
// can introduce tiny rounding errors, so we check "close enough"
// rather than exact equality.
const floatTolerance = 0.001

// closeEnough returns true if two floats are within the tolerance.
func closeEnough(a, b float64) bool {
	return math.Abs(a-b) <= floatTolerance
}

/* -------------------------------------------------------------- */
// CalculateUptime
/* -------------------------------------------------------------- */

func TestCalculateUptime(t *testing.T) {
	// Each test case has a name, a slice of heartbeat timestamps, and
	// the uptime percentage we expect the function to produce.
	var testCases = []struct {
		name           string
		heartbeats     []time.Time
		expectedUptime float64
	}{
		{
			name:           "empty list -- no data, return zero",
			heartbeats:     []time.Time{},
			expectedUptime: 0,
		},
		{
			name:           "single timestamp -- no span to measure, return zero",
			heartbeats:     []time.Time{minutesAfter(0)},
			expectedUptime: 0,
		},
		{
			name: "two heartbeats within the same minute -- return 100%",
			// Both timestamps truncate to the same minute bucket.
			// A zero span means we observed the device the whole time.
			heartbeats: []time.Time{
				baseTime.Add(5 * time.Second),  // 10:00:05
				baseTime.Add(50 * time.Second), // 10:00:50 -- same minute
			},
			expectedUptime: 100.0,
		},
		{
			name: "two heartbeats four minutes apart -- 50%",
			// buckets = {min0, min4} = 2, span = 4 minutes -> 50%
			// Minutes 1, 2, and 3 had no heartbeat.
			heartbeats: []time.Time{
				minutesAfter(0),
				minutesAfter(4),
			},
			expectedUptime: 50.0,
		},
		{
			name: "three heartbeats, one gap at the end -- 75%",
			// buckets = {min0, min1, min4} = 3, span = 4 minutes -> 75%
			// Device was offline at minutes 2 and 3, back online at 4.
			heartbeats: []time.Time{
				minutesAfter(0),
				minutesAfter(1),
				minutesAfter(4),
			},
			expectedUptime: 75.0,
		},
		{
			name: "duplicate timestamps in same minute deduplicate -- 50%",
			// Two records in minute 0 count as ONE covered minute, not two.
			// buckets = {min0, min4} = 2, span = 4 minutes -> 50%
			heartbeats: []time.Time{
				baseTime.Add(10 * time.Second), // 10:00:10 -- minute 0
				baseTime.Add(50 * time.Second), // 10:00:50 -- also minute 0
				minutesAfter(4),                // 10:04:00 -- minute 4
			},
			expectedUptime: 50.0,
		},
		{
			name: "four heartbeats filling a four-minute span -- 100%",
			// buckets = {min0, min1, min2, min4} = 4, span = 4 -> 100%
			// The gap at min3 doesn't reduce uptime because covered
			// minutes equals the span.
			heartbeats: []time.Time{
				minutesAfter(0),
				minutesAfter(1),
				minutesAfter(2),
				minutesAfter(4),
			},
			expectedUptime: 100.0,
		},
	}

	for _, tc := range testCases {
		// t.Run creates a named sub-test. If one fails, the others
		// still run. The name is printed on failure so you know
		// exactly which case broke.
		t.Run(tc.name, func(t *testing.T) {
			var actualUptime float64 = metrics.CalculateUptime(tc.heartbeats)
			if !closeEnough(actualUptime, tc.expectedUptime) {
				t.Errorf("got %.5f%%, want %.5f%%", actualUptime, tc.expectedUptime)
			}
		})
	}
}

/* -------------------------------------------------------------- */
// CalculateAverageUploadDuration
/* -------------------------------------------------------------- */

func TestCalculateAverageUploadDuration(t *testing.T) {
	var testCases = []struct {
		name              string
		uploadNanoseconds []int64
		expectedString    string // we verify the duration's string form
	}{
		{
			name:              "empty list returns zero duration",
			uploadNanoseconds: []int64{},
			expectedString:    "0s",
		},
		{
			name: "single value returns that value as a duration",
			// int64(5 * time.Second) converts the time.Duration constant
			// to a plain int64 nanosecond count, which is what the
			// function expects.
			uploadNanoseconds: []int64{int64(5 * time.Second)},
			expectedString:    "5s",
		},
		{
			name: "two equal values returns the same value",
			uploadNanoseconds: []int64{
				int64(3 * time.Minute),
				int64(3 * time.Minute),
			},
			expectedString: "3m0s",
		},
		{
			name: "average of 1 minute and 3 minutes is 2 minutes",
			uploadNanoseconds: []int64{
				int64(1 * time.Minute),
				int64(3 * time.Minute),
			},
			expectedString: "2m0s",
		},
		{
			name: "realistic nanosecond value formats correctly",
			// 197331667813 ns is approximately 3 minutes and 17 seconds.
			uploadNanoseconds: []int64{197331667813},
			expectedString:    "3m17.331667813s",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var actualDuration time.Duration = metrics.CalculateAverageUploadDuration(tc.uploadNanoseconds)
			var actualString string = actualDuration.String()
			if actualString != tc.expectedString {
				t.Errorf("got %q, want %q", actualString, tc.expectedString)
			}
		})
	}
}

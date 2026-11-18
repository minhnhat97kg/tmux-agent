// Package clock gathers every place that reads the system clock into one
// spot, and allows time to be frozen in tests.
//
// A golden frame contains "3s ago", an HH:MM time, a spinner frame, and an
// animation frame. Without freezing, a golden file could never be stable.
// The two environment variables below are ONLY used in tests; production
// code never sets them.
package clock

import (
	"os"
	"strconv"
	"time"
)

const (
	// EnvNow freezes the clock at a given epoch-seconds mark.
	EnvNow = "TMUX_AGENT_NOW"
	// EnvFrame freezes the animation frame counter.
	EnvFrame = "TMUX_AGENT_FRAME"
)

// Now returns the current time, or the frozen mark.
func Now() time.Time {
	return time.Unix(NowUnix(), 0)
}

// NowUnix returns epoch seconds. An env value that fails to parse is
// ignored.
func NowUnix() int64 {
	if frozen, ok := frozenEpoch(); ok {
		return frozen
	}
	return time.Now().Unix()
}

// NowUnixMilli returns epoch milliseconds. Used for the notification run id,
// where two SessionStart events close together on the same pane need
// different ids.
func NowUnixMilli() int64 {
	if frozen, ok := frozenEpoch(); ok {
		return frozen * 1000
	}
	return time.Now().UnixMilli()
}

// Frame returns the frozen animation frame counter, defaulting to 0.
// The real running path keeps its own counter and never calls this
// function.
func Frame() int {
	n, err := strconv.Atoi(os.Getenv(EnvFrame))
	if err != nil {
		return 0
	}
	return n
}

func frozenEpoch() (int64, bool) {
	raw := os.Getenv(EnvNow)
	if raw == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

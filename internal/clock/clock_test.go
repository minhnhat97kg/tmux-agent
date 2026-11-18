package clock

import (
	"testing"
	"time"
)

func TestNowUnixUsesWallClockByDefault(t *testing.T) {
	t.Setenv(EnvNow, "")

	got := NowUnix()
	want := time.Now().Unix()
	if got < want-2 || got > want+2 {
		t.Fatalf("NowUnix() = %d, want around %d", got, want)
	}
}

func TestNowUnixFrozenByEnv(t *testing.T) {
	t.Setenv(EnvNow, "1700000000")

	if got := NowUnix(); got != 1700000000 {
		t.Fatalf("NowUnix() = %d, want 1700000000", got)
	}
	if got := Now().Unix(); got != 1700000000 {
		t.Fatalf("Now().Unix() = %d, want 1700000000", got)
	}
	if got := NowUnixMilli(); got != 1700000000000 {
		t.Fatalf("NowUnixMilli() = %d, want 1700000000000", got)
	}
}

func TestNowUnixIgnoresGarbageValue(t *testing.T) {
	t.Setenv(EnvNow, "not-a-number")

	got := NowUnix()
	want := time.Now().Unix()
	if got < want-2 || got > want+2 {
		t.Fatalf("garbage value must be ignored; NowUnix() = %d, want around %d", got, want)
	}
}

func TestFrameDefaultsToZeroAndFreezes(t *testing.T) {
	t.Setenv(EnvFrame, "")
	if got := Frame(); got != 0 {
		t.Fatalf("Frame() = %d, want 0 when env is not set", got)
	}

	t.Setenv(EnvFrame, "7")
	if got := Frame(); got != 7 {
		t.Fatalf("Frame() = %d, want 7", got)
	}
}

// The size clamp's boundary is 8796093022207 MiB, and writing that down needs
// an int wider than 32 bits: on a 32-bit int the conversion is a compile error,
// which is why this one test is scoped rather than living beside the rest of
// the conversion table in rotate_test.go. The non-test package builds fine
// there, so the tests have to as well.
//
// Nothing is lost by scoping it. The values being pinned are inherently 64-bit:
// the largest setting a 32-bit int can hold is 2147483647 MiB, which is below
// maxRotateSizeMiB, so the clamp is unreachable there and
// rotateSizeBytes(math.MaxInt) — asserted portably in rotate_test.go — is the
// whole of what that platform can express.
//
// The constraint names the GOARCHes where int is 32 bits rather than listing
// the 64-bit ones, so a future 64-bit port gets this test without an edit.
//go:build !386 && !arm && !mips && !mipsle

package logging

import (
	"math"
	"testing"
	"time"
)

// TestRotateSizeClampHoldsAtItsBoundary pins the two values either side of the
// size clamp. One MiB more than maxRotateSizeMiB is where the unclamped
// multiplication wraps negative, and a negative threshold does not disable
// rotation — dueForRotation compares >=, so every write would rotate.
func TestRotateSizeClampHoldsAtItsBoundary(t *testing.T) {
	cases := map[int]int64{
		int(maxRotateSizeMiB):     maxRotateSizeMiB * bytesPerMiB,
		int(maxRotateSizeMiB) + 1: math.MaxInt64,
		math.MaxInt64:             math.MaxInt64,
	}

	for mib, want := range cases {
		if got := rotateSizeBytes(mib); got != want {
			t.Errorf("rotateSizeBytes(%d) = %d, want %d", mib, got, want)
		}

		if got := rotateSizeBytes(mib); got < 0 {
			t.Errorf("rotateSizeBytes(%d) wrapped to %d, which rotates on every write", mib, got)
		}
	}
}

// TestRotateAgeClampHoldsAtTheWidestInput is the age half of the same boundary
// at a width only this platform can express. rotate_test.go pins the clamp's
// own edge, which fits a 32-bit int; this pins that the widest input the type
// admits is still clamped rather than wrapped.
func TestRotateAgeClampHoldsAtTheWidestInput(t *testing.T) {
	if got := rotateAge(math.MaxInt64); got != math.MaxInt64 {
		t.Errorf("rotateAge(math.MaxInt64) = %v, want the clamp %v", got, time.Duration(math.MaxInt64))
	}
}

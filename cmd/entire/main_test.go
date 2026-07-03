package main

import (
	"os"
	"syscall"
	"testing"
)

// nonNumericSignal is an os.Signal that isn't a syscall.Signal, exercising
// exitCodeForSignal's fallback branch.
type nonNumericSignal struct{}

func (nonNumericSignal) String() string { return "non-numeric" }
func (nonNumericSignal) Signal()        {}

// TestExitCodeForSignal locks the conventional 128+signum mapping the
// Ctrl-C/SIGTERM fix relies on, so a future "simplification" back to a
// hardcoded 130 can't silently regress SIGTERM's 143.
func TestExitCodeForSignal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		sig  os.Signal
		want int
	}{
		{"SIGINT", os.Interrupt, 130},
		{"SIGTERM", syscall.SIGTERM, 143},
		{"non-numeric signal falls back to 130", nonNumericSignal{}, 130},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := exitCodeForSignal(tc.sig); got != tc.want {
				t.Errorf("exitCodeForSignal(%v) = %d, want %d", tc.sig, got, tc.want)
			}
		})
	}
}

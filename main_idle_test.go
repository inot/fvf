package main

import (
	"testing"
	"time"
)

func TestParseFlags_SetsFixedIdleTimeout(t *testing.T) {
	// Ensure opts.idleExitAfter is set to fixed 5 minutes regardless of flags
	opts := parseFlagsWithArgs([]string{"-timeout", "10s"})
	if opts.idleExitAfter != 5*time.Minute {
		t.Fatalf("idleExitAfter = %v, want 5m", opts.idleExitAfter)
	}
}

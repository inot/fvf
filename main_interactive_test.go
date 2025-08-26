package main

import "testing"

func TestDetermineInteractive_DefaultNoArgs(t *testing.T) {
	// No args -> interactive regardless of TTY
	if !determineInteractive(options{}, 0, false) {
		t.Fatalf("expected interactive when no args")
	}
}

func TestDetermineInteractive_TTYWithValues(t *testing.T) {
	opts := options{printValues: true}
	if !determineInteractive(opts, 3, true) {
		t.Fatalf("expected interactive when stdout is TTY and -values is set")
	}
}

func TestDetermineInteractive_TTYWithJSON(t *testing.T) {
	opts := options{jsonOut: true}
	if !determineInteractive(opts, 2, true) {
		t.Fatalf("expected interactive when stdout is TTY and -json is set")
	}
}

func TestDetermineInteractive_ExplicitInteractive(t *testing.T) {
	opts := options{interactive: true}
	if !determineInteractive(opts, 2, false) {
		t.Fatalf("expected interactive when -interactive is set")
	}
}

func TestDetermineInteractive_NonInteractive(t *testing.T) {
	// Some args, not TTY, no interactive flags -> follow opts.interactive (false)
	if determineInteractive(options{}, 2, false) {
		t.Fatalf("expected non-interactive when args present, not TTY, no flags")
	}
}

package prefs

import "testing"

func TestNextUIScaleWalksTheLadder(t *testing.T) {
	if got := NextUIScale(1.0, +1); got != 1.15 {
		t.Errorf("zoom in from 1.0 gave %v, want 1.15", got)
	}
	if got := NextUIScale(1.0, -1); got != 0.85 {
		t.Errorf("zoom out from 1.0 gave %v, want 0.85", got)
	}
	// The ends clamp rather than wrapping or running off.
	if got := NextUIScale(2.0, +1); got != 2.0 {
		t.Errorf("zoom in at the maximum gave %v, want 2.0", got)
	}
	if got := NextUIScale(0.75, -1); got != 0.75 {
		t.Errorf("zoom out at the minimum gave %v, want 0.75", got)
	}
}

// A value between stops — a hand-edited preferences file — must still move in
// the direction asked for rather than sticking.
func TestNextUIScaleSnapsOffLadderValues(t *testing.T) {
	if got := NextUIScale(1.07, +1); got <= 1.07 {
		t.Errorf("zoom in from 1.07 gave %v, which is not larger", got)
	}
	if got := NextUIScale(1.07, -1); got >= 1.07 {
		t.Errorf("zoom out from 1.07 gave %v, which is not smaller", got)
	}
	if got := NextUIScale(9.0, -1); got > MaxUIScale {
		t.Errorf("an absurd value did not come back into range: %v", got)
	}
}

func TestNormalizeFillsAndClampsScale(t *testing.T) {
	// A preferences file from before this setting existed has no value at all.
	if got := (Prefs{}).normalized().UIScale; got != 1 {
		t.Errorf("a missing scale became %v, want 1", got)
	}
	if got := (Prefs{UIScale: 0.1}).normalized().UIScale; got != MinUIScale {
		t.Errorf("a tiny scale became %v, want %v", got, MinUIScale)
	}
	if got := (Prefs{UIScale: 99}).normalized().UIScale; got != MaxUIScale {
		t.Errorf("a huge scale became %v, want %v", got, MaxUIScale)
	}
}

package scanstore

import "testing"

// The rules the progress bar has to obey are easier to state than to read off
// the walk, so they are pinned here directly.
func TestFractionRules(t *testing.T) {
	const gb = int64(1) << 30

	cases := []struct {
		name     string
		expected int64
		before   float64 // fraction already reached
		bytes    int64
		want     float64
	}{
		{
			name:     "no expectation stays indeterminate",
			expected: 0,
			before:   Indeterminate,
			bytes:    10 * gb,
			want:     Indeterminate,
		}, {
			// A denominator this small is noise from a stat of something
			// trivial, not a measurement worth dividing by.
			name:     "a tiny expectation is not an expectation",
			expected: 4096,
			before:   Indeterminate,
			bytes:    2048,
			want:     Indeterminate,
		}, {
			name:     "half way through reads as half",
			expected: 100 * gb,
			before:   0,
			bytes:    50 * gb,
			want:     0.5,
		}, {
			// Only a finished scan earns a full bar; while it runs there is
			// always the possibility of more.
			name:     "a running scan never quite reaches full",
			expected: 100 * gb,
			before:   0,
			bytes:    100 * gb,
			want:     runningCap,
		}, {
			// The denominator is a volume's used bytes, which no walk can
			// reach: it also covers snapshots and trees we were refused.
			name:     "an overshooting denominator lands just short",
			expected: 100 * gb,
			before:   0.9,
			bytes:    95 * gb,
			want:     0.95,
		}, {
			name:     "never goes backwards",
			expected: 100 * gb,
			before:   0.6,
			bytes:    50 * gb,
			want:     0.6,
		}, {
			// The folder grew since the cached scan this expectation came
			// from, so the expectation is disproved, not just imprecise.
			name:     "a disproved expectation gives up rather than lying",
			expected: 100 * gb,
			before:   runningCap,
			bytes:    200 * gb,
			want:     Indeterminate,
		}, {
			name:     "small overshoot holds rather than flickering",
			expected: 100 * gb,
			before:   runningCap,
			bytes:    102 * gb,
			want:     runningCap,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &Session{expectedBytes: c.expected, frac: c.before}
			got := s.fractionLocked(c.bytes)
			if diff := got - c.want; diff > 1e-9 || diff < -1e-9 {
				t.Errorf("fraction = %v, want %v", got, c.want)
			}
		})
	}
}

// Once an expectation is disproved it stays disproved: bytes only ever grow,
// so a bar that recovered to a number would be claiming knowledge it lost.
func TestDisprovedExpectationStaysIndeterminate(t *testing.T) {
	const gb = int64(1) << 30
	s := &Session{expectedBytes: 100 * gb, frac: 0.9}

	for _, bytes := range []int64{200 * gb, 300 * gb, 400 * gb} {
		s.frac = s.fractionLocked(bytes)
		if s.frac != Indeterminate {
			t.Fatalf("at %d GB the bar claimed %v", bytes/gb, s.frac)
		}
	}
}

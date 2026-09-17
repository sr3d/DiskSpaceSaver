package main

import "testing"

// The containment guard is the last thing between a bound call and a delete,
// and its Windows behaviour cannot be reached from a test running on macOS —
// filepath.Separator is baked in. pathWithin takes the separator instead, so
// both shapes are checked everywhere the suite runs.
func TestPathWithin(t *testing.T) {
	cases := []struct {
		name string
		root string
		path string
		sep  string
		want bool
	}{
		// A whole drive on Windows: the case that refused every delete.
		{"windows drive root", `C:\`, `C:\Users\ale\big.iso`, `\`, true},
		{"windows drive root, child dir", `C:\`, `C:\Users`, `\`, true},
		{"windows drive root itself", `C:\`, `C:\`, `\`, false},
		{"windows folder scan", `C:\Users\ale`, `C:\Users\ale\big.iso`, `\`, true},
		{"windows folder scan, root itself", `C:\Users\ale`, `C:\Users\ale`, `\`, false},
		{"windows sibling with shared prefix", `C:\Users\ale`, `C:\Users\alex\big.iso`, `\`, false},
		{"windows other drive", `C:\`, `D:\big.iso`, `\`, false},
		{"windows unc share", `\\nas\media`, `\\nas\media\film.mkv`, `\`, true},

		// Unix, including the "/" case the old special branch covered.
		{"unix filesystem root", "/", "/Users/ale/big.iso", "/", true},
		{"unix filesystem root itself", "/", "/", "/", false},
		{"unix folder scan", "/Users/ale", "/Users/ale/big.iso", "/", true},
		{"unix folder scan, root itself", "/Users/ale", "/Users/ale", "/", false},
		{"unix sibling with shared prefix", "/Users/ale", "/Users/alex/big.iso", "/", false},
		{"unix escape", "/Users/ale", "/etc/passwd", "/", false},
	}
	for _, c := range cases {
		if got := pathWithin(c.root, c.path, c.sep); got != c.want {
			t.Errorf("%s: pathWithin(%q, %q, %q) = %v, want %v", c.name, c.root, c.path, c.sep, got, c.want)
		}
	}
}

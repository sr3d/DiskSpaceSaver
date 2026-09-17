package main

import "testing"

// The rule that decides whether a ticked item's bytes count towards the
// selection total. A plain prefix test would call "/v/about.txt" a child of
// "/v/a" and quietly drop a sibling's bytes from the total.
func TestPathContains(t *testing.T) {
	cases := []struct {
		parent, child string
		want          bool
	}{
		{"/v/a", "/v/a/b", true},
		{"/v/a", "/v/a/b/c", true},
		{"/v/a", "/v/about.txt", false},
		{"/v/a", "/v/a", false},
		{"/v/a/b", "/v/a", false},
		{"", "/v/a", false},
		{"/v/a", "", false},
		{"/", "/v", true},
	}
	for _, c := range cases {
		if got := pathContains(c.parent, c.child); got != c.want {
			t.Errorf("pathContains(%q, %q) = %v, want %v", c.parent, c.child, got, c.want)
		}
	}
}

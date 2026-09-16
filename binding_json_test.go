package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sr3d/diskspacesaver/internal/scanner"
	"github.com/sr3d/diskspacesaver/internal/tree"
)

// A nil Go slice marshals to JSON null, and Wails' generated model
// constructors index into the payload without checking. Anything the frontend
// iterates therefore has to be an allocated slice even when it is empty, or
// the binding throws before any of our code runs.
func TestBoundResultsNeverMarshalNullCollections(t *testing.T) {
	tr := tree.New("/scan")
	tr.Apply(scanner.Frame{DirID: scanner.RootDirID, Entry: []scanner.Entry{
		{Name: "a.bin", Kind: scanner.KindFile, Alloc: 10},
	}})
	tr.Finalize()

	cases := []struct {
		name string
		v    any
	}{
		{"viewport at root (no ancestors)", tr.Viewport(tr.Root, 5, 0, false)},
		{"children of a leaf", tr.Children(tr.Lookup("/scan/a.bin"), 0, 10)},
		{"children of a missing node", tr.Children(9999, 0, 10)},
		{"children past the end", tr.Children(tr.Root, 500, 10)},
		{"ancestors of a missing node", tr.Ancestors(9999)},
		{"empty batch result", newResult()},
		{"cache miss", CachedScan{}},
	}

	for _, c := range cases {
		b, err := json.Marshal(c.v)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if strings.Contains(string(b), ":null") {
			t.Errorf("%s marshals a null collection, which throws in the generated binding:\n  %s", c.name, b)
		}
		// A bare "null" document is the nil-pointer case.
		if string(b) == "null" {
			t.Errorf("%s marshals to bare null", c.name)
		}
	}
}

package collector

import "testing"

func TestAddDeduplicatesAndSubsumes(t *testing.T) {
	c := New()
	c.Add(Item{ScanID: "s", NodeID: 1, Path: "/v/a/b.bin", Name: "b.bin", Size: 100})
	c.Add(Item{ScanID: "s", NodeID: 1, Path: "/v/a/b.bin", Name: "b.bin", Size: 100})
	if st := c.List(); st.Count != 1 || st.Total != 100 {
		t.Fatalf("duplicate add changed the tray: %+v", st)
	}

	// Adding the parent must absorb the child rather than double-count it.
	st := c.Add(Item{ScanID: "s", NodeID: 2, Path: "/v/a", Name: "a", Size: 500})
	if st.Count != 1 || st.Total != 500 {
		t.Fatalf("parent did not subsume child: %+v", st)
	}

	// Adding the child back under a collected parent is a no-op.
	st = c.Add(Item{ScanID: "s", NodeID: 3, Path: "/v/a/c.bin", Size: 40})
	if st.Count != 1 || st.Total != 500 {
		t.Fatalf("child added under a collected parent: %+v", st)
	}

	// A sibling with a shared name prefix is not a descendant.
	st = c.Add(Item{ScanID: "s", NodeID: 4, Path: "/v/about.txt", Size: 7})
	if st.Count != 2 {
		t.Fatalf("prefix sibling treated as descendant: %+v", st)
	}
}

func TestCrossScanIsolation(t *testing.T) {
	c := New()
	c.Add(Item{ScanID: "a", NodeID: 1, Path: "/v/x", Size: 10})
	c.Add(Item{ScanID: "b", NodeID: 1, Path: "/v/x/y", Size: 5})
	if st := c.List(); st.Count != 2 {
		t.Fatalf("subsumption leaked across scans: %+v", st)
	}
	if st := c.RemoveScan("a"); st.Count != 1 || st.Total != 5 {
		t.Fatalf("RemoveScan wrong: %+v", st)
	}
	if st := c.Clear(); st.Count != 0 || st.Total != 0 {
		t.Fatalf("Clear wrong: %+v", st)
	}
}

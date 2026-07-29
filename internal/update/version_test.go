package update

import "testing"

// TestParseAcceptsEveryShapeATagTakes covers the four shapes release.yml lets
// through: 2.1.0, v2.1.0, 2.1 and 2.0.1-rc1.
func TestParseAcceptsEveryShapeATagTakes(t *testing.T) {
	cases := []struct {
		raw     string
		want    Version
		display string
	}{
		{"2.1.0", Version{Major: 2, Minor: 1, Patch: 0}, "2.1.0"},
		{"v2.1.0", Version{Major: 2, Minor: 1, Patch: 0}, "2.1.0"},
		{"2.1", Version{Major: 2, Minor: 1, Patch: 0}, "2.1.0"},
		{"v0.1", Version{Major: 0, Minor: 1, Patch: 0}, "0.1.0"},
		{"2.0.1-rc1", Version{Major: 2, Minor: 0, Patch: 1, Pre: "rc1"}, "2.0.1-rc1"},
	}
	for _, c := range cases {
		got, err := ParseVersion(c.raw)
		if err != nil {
			t.Fatalf("ParseVersion(%q) : %v", c.raw, err)
		}
		if got != c.want {
			t.Errorf("ParseVersion(%q) = %+v, attendu %+v", c.raw, got, c.want)
		}
		if got.String() != c.display {
			t.Errorf("String() de %q = %q, attendu %q", c.raw, got.String(), c.display)
		}
	}
}

// TestParseRefusesWhatIsNotAVersion keeps a branch name or a working tag from
// ever being read as one.
func TestParseRefusesWhatIsNotAVersion(t *testing.T) {
	for _, raw := range []string{"", "banc-de-test", "avant-migration", "2", "v",
		"2.1.0.0", "2.-1.0", "2.1.0 ", "deux.un"} {
		if _, err := ParseVersion(raw); err == nil {
			t.Errorf("ParseVersion(%q) a accepté ce qui n'est pas une version", raw)
		}
	}
}

// TestCompareOrdersTheReleasesAStationCouldSee asserts the one property the
// button depends on: is what is published newer than what runs?
func TestCompareOrdersTheReleasesAStationCouldSee(t *testing.T) {
	cases := []struct {
		left, right string
		want        int
	}{
		{"2.1.0", "2.0.3", +1},
		{"2.0.3", "2.1.0", -1},
		{"2.1.0", "2.1.0", 0},
		{"2.1", "2.1.0", 0},
		{"v2.1.0", "2.1.0", 0},
		{"3.0.0", "2.99.99", +1},
		// A prerelease is BELOW its own release: 2.1.0-rc1 comes before 2.1.0.
		{"2.1.0-rc1", "2.1.0", -1},
		{"2.1.0", "2.1.0-rc1", +1},
		{"2.1.0-rc2", "2.1.0-rc1", +1},
	}
	for _, c := range cases {
		left, err := ParseVersion(c.left)
		if err != nil {
			t.Fatalf("ParseVersion(%q) : %v", c.left, err)
		}
		right, err := ParseVersion(c.right)
		if err != nil {
			t.Fatalf("ParseVersion(%q) : %v", c.right, err)
		}
		if got := left.Compare(right); got != c.want {
			t.Errorf("%q.Compare(%q) = %d, attendu %d", c.left, c.right, got, c.want)
		}
	}
}

// TestAPrereleaseSaysSo is what keeps a release candidate off a station.
func TestAPrereleaseSaysSo(t *testing.T) {
	stable, err := ParseVersion("2.1.0")
	if err != nil {
		t.Fatalf("ParseVersion : %v", err)
	}
	candidate, err := ParseVersion("2.1.0-rc1")
	if err != nil {
		t.Fatalf("ParseVersion : %v", err)
	}
	if stable.IsPrerelease() {
		t.Error("2.1.0 se déclare préversion")
	}
	if !candidate.IsPrerelease() {
		t.Error("2.1.0-rc1 ne se déclare pas préversion")
	}
}

// TestTheVersionOfADevelopmentBuildIsNotAVersion: main.go ships « dev » until a
// tag is built, and a station running it must not be told it is out of date by a
// comparison that read « dev » as zero.
func TestTheVersionOfADevelopmentBuildIsNotAVersion(t *testing.T) {
	if _, err := ParseVersion("dev"); err == nil {
		t.Fatal("« dev » est lu comme une version")
	}
}

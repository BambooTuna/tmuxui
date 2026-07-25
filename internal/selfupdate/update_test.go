package selfupdate

import "testing"

func TestCompareSemver(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v1.2.3", "v1.2.3", 0},
		{"v1.2.3", "v1.2.4", -1},
		{"v1.2.4", "v1.2.3", 1},
		{"v1.3.0", "v1.2.9", 1},
		{"v2.0.0", "v1.9.9", 1},
		// "v" prefix is optional on either side.
		{"1.2.3", "v1.2.3", 0},
		// pre-release suffixes are ignored by the simplified comparator (documented behavior:
		// parseSemverTriple strips everything from '-'/'+' onward), so a prerelease tag compares
		// equal to its base version.
		{"v2.0.0-rc.1", "v2.0.0", 0},
		{"v2.0.0-rc.1", "v1.9.9", 1},
		// non-semver strings parse as all-zero and compare equal to each other.
		{"not-a-version", "also-not-a-version", 0},
		{"not-a-version", "v0.0.1", -1},
	}
	for _, c := range cases {
		if got := compareSemver(c.a, c.b); got != c.want {
			t.Errorf("compareSemver(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestCompareSemverIsAntisymmetric(t *testing.T) {
	// compareSemver(a,b) and compareSemver(b,a) should have opposite signs whenever a != b,
	// which the selectableMinVersion filtering (ListSelectableReleases) and ApplyVersion's
	// minimum-version gate both rely on.
	pairs := [][2]string{
		{"v1.0.0", "v2.0.0"},
		{"v1.0.0", "v1.0.1"},
		{"v1.5.2", "v1.5.10"},
	}
	for _, p := range pairs {
		ab := compareSemver(p[0], p[1])
		ba := compareSemver(p[1], p[0])
		if ab == 0 || ba == 0 || (ab < 0) == (ba < 0) {
			t.Errorf("compareSemver(%q,%q)=%d and compareSemver(%q,%q)=%d are not opposite signs", p[0], p[1], ab, p[1], p[0], ba)
		}
	}
}

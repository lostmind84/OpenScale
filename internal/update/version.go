package update

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
)

// ErrNotAVersion reports a tag that is not a version number.
//
// It exists because a repository carries working tags -- `banc-de-test`,
// `avant-migration` -- alongside its releases, and because a binary built off a
// tag calls itself « dev ». Reading either as a version would offer a station an
// update to nothing, or tell a station running « dev » that it is out of date.
var ErrNotAVersion = errors.New("update: not a version")

// versionShape is the exact set release.yml lets through, and no more.
//
// It is deliberately the same shape the workflow validates before it builds:
// what a release can be named and what a station can install are one question,
// and answering it twice differently is how the two drift apart.
var versionShape = regexp.MustCompile(`^v?([0-9]+)\.([0-9]+)(?:\.([0-9]+))?(?:-([A-Za-z0-9.]+))?$`)

// Version is a release number, ordered.
//
// Patch is normalised: the tag v0.1 and the tag 0.1.0 name the same release, and
// the first published version of this repository was called v0.1.
type Version struct {
	Major, Minor, Patch int
	// Pre is the suffix of a prerelease, without its dash, or the empty string.
	Pre string
}

// ParseVersion reads a git tag as a version number.
func ParseVersion(s string) (Version, error) {
	parts := versionShape.FindStringSubmatch(s)
	if parts == nil {
		return Version{}, fmt.Errorf("%w: %q", ErrNotAVersion, s)
	}
	// None of the three can fail: the expression only matched digits. They can
	// still overflow on an absurd tag, and Atoi reports that as an error, which
	// is why the results are checked rather than discarded.
	major, err := strconv.Atoi(parts[1])
	if err != nil {
		return Version{}, fmt.Errorf("%w: %q", ErrNotAVersion, s)
	}
	minor, err := strconv.Atoi(parts[2])
	if err != nil {
		return Version{}, fmt.Errorf("%w: %q", ErrNotAVersion, s)
	}
	patch := 0
	if parts[3] != "" {
		if patch, err = strconv.Atoi(parts[3]); err != nil {
			return Version{}, fmt.Errorf("%w: %q", ErrNotAVersion, s)
		}
	}
	return Version{Major: major, Minor: minor, Patch: patch, Pre: parts[4]}, nil
}

// IsPrerelease reports a version a station is never offered.
func (v Version) IsPrerelease() bool { return v.Pre != "" }

// Compare orders two versions: -1 when v is older, 0 when they name the same
// release, +1 when v is newer.
//
// A prerelease sorts BELOW its own release -- 2.1.0-rc1 before 2.1.0 -- which is
// the rule of semantic versioning and the only one that keeps « is there
// something newer? » from answering yes to a candidate the station just left.
func (v Version) Compare(other Version) int {
	for _, pair := range [][2]int{
		{v.Major, other.Major}, {v.Minor, other.Minor}, {v.Patch, other.Patch},
	} {
		switch {
		case pair[0] < pair[1]:
			return -1
		case pair[0] > pair[1]:
			return +1
		}
	}
	switch {
	case v.Pre == other.Pre:
		return 0
	case v.Pre == "":
		return +1
	case other.Pre == "":
		return -1
	case v.Pre < other.Pre:
		return -1
	default:
		return +1
	}
}

// String renders the version as the screen shows it, without the leading « v ».
func (v Version) String() string {
	base := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Pre == "" {
		return base
	}
	return base + "-" + v.Pre
}

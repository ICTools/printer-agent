// Package selfupdate finds, downloads, verifies and installs printer releases
// published on GitHub.
package selfupdate

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Version is a semantic version "vMAJOR.MINOR.PATCH[-PRERELEASE]".
type Version struct {
	Major, Minor, Patch int
	Prerelease          string
}

var semverRe = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?$`)

// gitDescribeRe matches the "-N-gSHA" suffix git describe adds after a tag.
var gitDescribeRe = regexp.MustCompile(`-\d+-g[0-9a-f]+(-dirty)?$`)

// ParseVersion parses a release version. Development builds ("dev", a bare
// commit hash, a git describe output such as "v1.2.0-3-gabc1234-dirty") are
// rejected: they are not releases and cannot be ordered against one.
func ParseVersion(s string) (Version, error) {
	s = strings.TrimSpace(s)
	if gitDescribeRe.MatchString(s) || strings.HasSuffix(s, "-dirty") {
		return Version{}, fmt.Errorf("%q is a development build", s)
	}
	m := semverRe.FindStringSubmatch(s)
	if m == nil {
		return Version{}, fmt.Errorf("%q is not a release version (vX.Y.Z)", s)
	}
	v := Version{Prerelease: m[4]}
	v.Major, _ = strconv.Atoi(m[1])
	v.Minor, _ = strconv.Atoi(m[2])
	v.Patch, _ = strconv.Atoi(m[3])
	return v, nil
}

func (v Version) String() string {
	s := fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Prerelease != "" {
		s += "-" + v.Prerelease
	}
	return s
}

// Compare returns -1, 0 or 1 as v is older than, equal to or newer than o,
// following the semver precedence rules (a prerelease precedes its release).
func (v Version) Compare(o Version) int {
	for _, d := range [][2]int{{v.Major, o.Major}, {v.Minor, o.Minor}, {v.Patch, o.Patch}} {
		if d[0] != d[1] {
			return cmpInt(d[0], d[1])
		}
	}
	return comparePrerelease(v.Prerelease, o.Prerelease)
}

func comparePrerelease(a, b string) int {
	switch {
	case a == b:
		return 0
	case a == "":
		return 1
	case b == "":
		return -1
	}
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		if pa[i] == pb[i] {
			continue
		}
		na, errA := strconv.Atoi(pa[i])
		nb, errB := strconv.Atoi(pb[i])
		switch {
		case errA == nil && errB == nil:
			return cmpInt(na, nb)
		case errA == nil: // numeric identifiers have lower precedence
			return -1
		case errB == nil:
			return 1
		default:
			return strings.Compare(pa[i], pb[i])
		}
	}
	return cmpInt(len(pa), len(pb))
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// Decision is the outcome of comparing the running version with a release.
type Decision int

const (
	// UpToDate: the running version is the release or newer.
	UpToDate Decision = iota
	// UpdateAvailable: the release is newer than the running version.
	UpdateAvailable
	// DevBuild: the running binary is not a release; installing the release
	// replaces it with a published build.
	DevBuild
)

// Decide compares the running version with the latest release tag.
func Decide(current, latest string) (Decision, error) {
	lv, err := ParseVersion(latest)
	if err != nil {
		return UpToDate, fmt.Errorf("latest release: %w", err)
	}
	cv, err := ParseVersion(current)
	if err != nil {
		return DevBuild, nil
	}
	if lv.Compare(cv) > 0 {
		return UpdateAvailable, nil
	}
	return UpToDate, nil
}

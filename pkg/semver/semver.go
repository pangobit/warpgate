// Package semver matches container image tags against Warpgate version constraints.
package semver

import (
	"fmt"
	"strings"

	xsemver "golang.org/x/mod/semver"
)

// Constraint selects acceptable semantic versions for an image.
type Constraint struct {
	raw             string
	op              string
	base            string
	allowPrerelease bool
}

// ParseConstraint parses a Warpgate image version constraint.
// Supported forms: "*" (any stable version), "1.2.3" (exact),
// "~1.2" or "~1.2.3" (same major.minor), and "^1" or "^1.2.3" (same major).
func ParseConstraint(raw string) (Constraint, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "*" {
		return Constraint{raw: trimmed, op: "*"}, nil
	}
	op := ""
	rest := trimmed
	if strings.HasPrefix(trimmed, "~") || strings.HasPrefix(trimmed, "^") {
		op = trimmed[:1]
		rest = trimmed[1:]
	}
	base := normalize(rest)
	if !xsemver.IsValid(base) {
		return Constraint{}, fmt.Errorf("invalid semver constraint %q", raw)
	}
	if op == "" && canonicalTag(rest) == "" {
		return Constraint{}, fmt.Errorf("invalid semver constraint %q: exact constraints require major.minor.patch", raw)
	}
	return Constraint{
		raw:             trimmed,
		op:              op,
		base:            base,
		allowPrerelease: xsemver.Prerelease(base) != "",
	}, nil
}

// String returns the constraint as written.
func (c Constraint) String() string {
	return c.raw
}

// Matches reports whether an image tag is a semantic version accepted by the constraint.
func (c Constraint) Matches(tag string) bool {
	version := canonicalTag(tag)
	if version == "" {
		return false
	}
	if xsemver.Prerelease(version) != "" && !c.allowPrerelease {
		return false
	}
	switch c.op {
	case "*":
		return true
	case "~":
		return xsemver.Compare(version, xsemver.Canonical(c.base)) >= 0 && xsemver.MajorMinor(version) == xsemver.MajorMinor(c.base)
	case "^":
		return xsemver.Compare(version, xsemver.Canonical(c.base)) >= 0 && xsemver.Major(version) == xsemver.Major(c.base)
	default:
		return xsemver.Compare(version, c.base) == 0
	}
}

// HighestMatch returns the highest tag accepted by the constraint.
func HighestMatch(tags []string, c Constraint) (string, bool) {
	best := ""
	bestVersion := ""
	for _, tag := range tags {
		if !c.Matches(tag) {
			continue
		}
		version := canonicalTag(tag)
		if best == "" || xsemver.Compare(version, bestVersion) > 0 {
			best = tag
			bestVersion = version
		}
	}
	return best, best != ""
}

// CompareTags compares two image tags as semantic versions.
// It reports ok=false when either tag is not a full semantic version.
func CompareTags(a string, b string) (int, bool) {
	left := canonicalTag(a)
	right := canonicalTag(b)
	if left == "" || right == "" {
		return 0, false
	}
	return xsemver.Compare(left, right), true
}

// canonicalTag returns the canonical "vX.Y.Z[-pre]" form of a tag, or "" when
// the tag is not a full major.minor.patch semantic version. Shorthand tags such
// as "1" or "1.2" are rejected because they are mutable floating tags.
func canonicalTag(tag string) string {
	version := normalize(tag)
	if !xsemver.IsValid(version) {
		return ""
	}
	if xsemver.Canonical(version) != version {
		return ""
	}
	return version
}

func normalize(tag string) string {
	trimmed := strings.TrimSpace(tag)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "v") {
		return trimmed
	}
	return "v" + trimmed
}

package manifest

import (
	"fmt"
	"strings"

	"golang.org/x/mod/semver"
)

// LogicalID forms the catalog identifier namespace.name.
func LogicalID(namespace, name string) string {
	ns := strings.TrimSpace(namespace)
	n := strings.TrimSpace(name)
	if ns == "" || n == "" {
		return ""
	}
	return ns + "." + n
}

// ParsedLogicalRef is a catalog reference with an optional semver constraint.
type ParsedLogicalRef struct {
	Namespace  string
	Name       string
	Constraint string // empty means any version
	Raw        string // canonical namespace.name
}

// ParseLogicalRef parses refs such as claims.approve-claim-payment or claims.approve-claim-payment@^1.3.
func ParseLogicalRef(ref string) (ParsedLogicalRef, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ParsedLogicalRef{}, fmt.Errorf("ref is empty")
	}
	constraint := ""
	if at := strings.LastIndex(ref, "@"); at > 0 {
		constraint = strings.TrimSpace(ref[at+1:])
		ref = strings.TrimSpace(ref[:at])
	}
	dot := strings.Index(ref, ".")
	if dot <= 0 || dot >= len(ref)-1 {
		return ParsedLogicalRef{}, fmt.Errorf("ref %q must be namespace.name", ref)
	}
	out := ParsedLogicalRef{
		Namespace:  ref[:dot],
		Name:       ref[dot+1:],
		Constraint: constraint,
		Raw:        ref,
	}
	return out, nil
}

// MatchesVersion reports whether toolVersion satisfies the ref constraint.
// An empty constraint accepts any valid semver tool version.
func (r ParsedLogicalRef) MatchesVersion(toolVersion string) error {
	constraint := strings.TrimSpace(r.Constraint)
	if constraint == "" {
		return nil
	}
	toolVersion = strings.TrimSpace(toolVersion)
	if toolVersion == "" {
		return fmt.Errorf("tool %q has no metadata.version", r.Raw)
	}
	if !isValidSemver(toolVersion) {
		return fmt.Errorf("tool %q metadata.version %q is not valid semver", r.Raw, toolVersion)
	}
	tv := semver.Canonical(normalizeSemver(toolVersion))
	cv := normalizeConstraint(constraint)
	if cv == "" {
		return fmt.Errorf("invalid semver constraint %q on ref %q", constraint, r.Raw)
	}
	if !semver.IsValid(cv) {
		return fmt.Errorf("invalid semver constraint %q on ref %q", constraint, r.Raw)
	}
	if semver.Compare(tv, cv) < 0 {
		return fmt.Errorf("tool %q version %q does not satisfy constraint %q", r.Raw, toolVersion, constraint)
	}
	if strings.HasPrefix(constraint, "^") {
		major := majorVersion(cv)
		if majorVersion(tv) != major {
			return fmt.Errorf("tool %q version %q does not satisfy constraint %q", r.Raw, toolVersion, constraint)
		}
	}
	return nil
}

func normalizeSemver(v string) string {
	v = strings.TrimSpace(v)
	if !strings.HasPrefix(v, "v") {
		return "v" + v
	}
	return v
}

func normalizeConstraint(constraint string) string {
	c := strings.TrimSpace(constraint)
	if c == "" {
		return ""
	}
	if strings.HasPrefix(c, "^") || strings.HasPrefix(c, "~") || strings.HasPrefix(c, ">") || strings.HasPrefix(c, "<") || strings.HasPrefix(c, "=") {
		c = strings.TrimLeft(c, "^~<>= ")
	}
	return semver.Canonical(normalizeSemver(c))
}

func majorVersion(v string) string {
	v = strings.TrimPrefix(v, "v")
	parts := strings.SplitN(v, ".", 2)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

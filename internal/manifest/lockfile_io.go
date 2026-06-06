package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const DefaultLockfileName = "bundle.lock.json"

// LockfilePath returns the committed lockfile path beside a bundle manifest.
func LockfilePath(bundleManifestPath string) string {
	return filepath.Join(filepath.Dir(bundleManifestPath), DefaultLockfileName)
}

// ReadLockfile parses bundle.lock.json from disk.
func ReadLockfile(path string) (*Lockfile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseLockfileJSON(raw)
}

// ParseLockfileJSON decodes canonical bundle.lock.json bytes.
func ParseLockfileJSON(raw []byte) (*Lockfile, error) {
	raw = trimJSON(raw)
	if len(raw) == 0 {
		return nil, fmt.Errorf("lockfile is empty")
	}
	var lock Lockfile
	if err := json.Unmarshal(raw, &lock); err != nil {
		return nil, fmt.Errorf("parse lockfile: %w", err)
	}
	return &lock, nil
}

// MarshalLockfile encodes a lockfile with a computed version field.
func MarshalLockfile(lock Lockfile) ([]byte, error) {
	version, err := LockfileVersion(lock.RootChildName, lock.Members)
	if err != nil {
		return nil, err
	}
	lock.Version = version
	raw, err := json.Marshal(lock)
	if err != nil {
		return nil, fmt.Errorf("encode lockfile: %w", err)
	}
	return raw, nil
}

// WriteLockfile writes canonical bundle.lock.json beside the bundle manifest directory.
func WriteLockfile(path string, lock Lockfile) error {
	raw, err := MarshalLockfile(lock)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return fmt.Errorf("write lockfile: %w", err)
	}
	return nil
}

// LockfileFromClosure extracts the lock body from a walked closure package.
func LockfileFromClosure(pkg *ClosurePackage) Lockfile {
	if pkg == nil {
		return Lockfile{}
	}
	return pkg.Lockfile
}

// CompareLockfiles returns an error when committed and recomputed lock bodies differ.
func CompareLockfiles(want, got Lockfile) error {
	if strings.TrimSpace(want.RootChildName) != strings.TrimSpace(got.RootChildName) {
		return fmt.Errorf("lockfile root_child_name drift: committed %q, recomputed %q",
			want.RootChildName, got.RootChildName)
	}
	if len(want.Members) != len(got.Members) {
		return fmt.Errorf("lockfile member count drift: committed %d, recomputed %d",
			len(want.Members), len(got.Members))
	}
	for i := range want.Members {
		if err := compareLockfileMember(i, want.Members[i], got.Members[i]); err != nil {
			return err
		}
	}
	wantVersion, err := LockfileVersion(want.RootChildName, want.Members)
	if err != nil {
		return err
	}
	gotVersion, err := LockfileVersion(got.RootChildName, got.Members)
	if err != nil {
		return err
	}
	if wantVersion != gotVersion {
		return fmt.Errorf("lockfile version drift: committed %q, recomputed %q", wantVersion, gotVersion)
	}
	return ValidateLockfileVersion(want)
}

func compareLockfileMember(index int, want, got LockfileMember) error {
	prefix := fmt.Sprintf("lockfile members[%d]", index)
	if want.ChildName != got.ChildName {
		return fmt.Errorf("%s child_name drift: committed %q, recomputed %q", prefix, want.ChildName, got.ChildName)
	}
	if want.Origin != got.Origin {
		return fmt.Errorf("%s origin drift: committed %q, recomputed %q", prefix, want.Origin, got.Origin)
	}
	if want.Ref != got.Ref {
		return fmt.Errorf("%s ref drift: committed %q, recomputed %q", prefix, want.Ref, got.Ref)
	}
	switch want.Origin {
	case ClosureMemberOriginVendored:
		if want.ContentHash != got.ContentHash {
			return fmt.Errorf("%s (%s) content_hash drift: lock is out of date; run phrony bundle lock",
				prefix, want.ChildName)
		}
	case ClosureMemberOriginExternal:
		if want.Namespace != got.Namespace {
			return fmt.Errorf("%s namespace drift: committed %q, recomputed %q", prefix, want.Namespace, got.Namespace)
		}
		if want.Name != got.Name {
			return fmt.Errorf("%s name drift: committed %q, recomputed %q", prefix, want.Name, got.Name)
		}
		if want.Version != got.Version {
			return fmt.Errorf("%s version drift: committed %q, recomputed %q", prefix, want.Version, got.Version)
		}
		if want.ContentHash != "" {
			return fmt.Errorf("%s (%s) must not include content_hash in committed lock; run phrony bundle lock",
				prefix, want.ChildName)
		}
		if got.ContentHash != "" {
			return fmt.Errorf("%s (%s) must not include content_hash in recomputed lock", prefix, want.ChildName)
		}
	default:
		return fmt.Errorf("%s unsupported origin %q", prefix, want.Origin)
	}
	return nil
}

// ValidateLockfileVersion checks that the version label matches the lock body hash.
func ValidateLockfileVersion(lock Lockfile) error {
	expected, err := LockfileVersion(lock.RootChildName, lock.Members)
	if err != nil {
		return err
	}
	if strings.TrimSpace(lock.Version) == "" {
		return errors.New("lockfile version is required")
	}
	if lock.Version != expected {
		return fmt.Errorf("lockfile version %q does not match body hash %q", lock.Version, expected)
	}
	return nil
}

func trimJSON(raw []byte) []byte {
	return []byte(strings.TrimSpace(string(raw)))
}

package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLockfileRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeClosureAgent(t, dir, "billing", "billing.yaml", "Handle billing.", nil)
	writeClosureAgent(t, dir, "orchestrator", "orchestrator.yaml", "Route tasks.", []SubagentBinding{{
		Ref: "./billing.yaml",
	}})
	bundle := writeBundleManifest(t, dir, "./orchestrator.yaml")

	pkg, err := WalkBundle(dir, bundle)
	if err != nil {
		t.Fatalf("WalkBundle: %v", err)
	}
	lock := LockfileFromClosure(pkg)
	lockPath := filepath.Join(dir, DefaultLockfileName)
	if err := WriteLockfile(lockPath, lock); err != nil {
		t.Fatalf("WriteLockfile: %v", err)
	}

	read, err := ReadLockfile(lockPath)
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	if err := CompareLockfiles(lock, *read); err != nil {
		t.Fatalf("CompareLockfiles after round-trip: %v", err)
	}
	if read.Version != pkg.Version {
		t.Fatalf("version = %q, want %q", read.Version, pkg.Version)
	}
}

func TestCompareLockfiles_detectsDrift(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeClosureAgent(t, dir, "billing", "billing.yaml", "Handle billing.", nil)
	writeClosureAgent(t, dir, "orchestrator", "orchestrator.yaml", "Route tasks.", []SubagentBinding{{
		Ref: "./billing.yaml",
	}})
	bundle := writeBundleManifest(t, dir, "./orchestrator.yaml")

	pkg, err := WalkBundle(dir, bundle)
	if err != nil {
		t.Fatalf("WalkBundle: %v", err)
	}
	committed := LockfileFromClosure(pkg)

	drifted := committed
	drifted.Members[1].ContentHash = "deadbeef"

	if err := CompareLockfiles(committed, drifted); err == nil {
		t.Fatal("CompareLockfiles() error = nil, want drift error")
	}
}

func TestCompareLockfiles_rejectsRecomputedExternalContentHash(t *testing.T) {
	t.Parallel()
	committed := Lockfile{
		RootChildName: "orchestrator",
		Members: []LockfileMember{{
			ChildName: "refunds",
			Origin:    ClosureMemberOriginExternal,
			Ref:       "billing.refunds@1.4.0",
			Namespace: "billing",
			Name:      "refunds",
			Version:   "1.4.0",
		}},
	}
	version, err := LockfileVersion(committed.RootChildName, committed.Members)
	if err != nil {
		t.Fatalf("LockfileVersion: %v", err)
	}
	committed.Version = version

	recomputed := committed
	recomputed.Members = append([]LockfileMember(nil), committed.Members...)
	recomputed.Members[0].ContentHash = "should-not-be-here"
	if err := CompareLockfiles(committed, recomputed); err == nil {
		t.Fatal("CompareLockfiles() error = nil, want recomputed external content_hash rejection")
	}

	if err := CompareLockfiles(committed, committed); err != nil {
		t.Fatalf("CompareLockfiles identical: %v", err)
	}
}

func TestCompareLockfiles_rejectsCommittedExternalContentHash(t *testing.T) {
	t.Parallel()
	clean := Lockfile{
		RootChildName: "orchestrator",
		Members: []LockfileMember{{
			ChildName: "refunds",
			Origin:    ClosureMemberOriginExternal,
			Ref:       "billing.refunds@1.4.0",
			Namespace: "billing",
			Name:      "refunds",
			Version:   "1.4.0",
		}},
	}
	version, err := LockfileVersion(clean.RootChildName, clean.Members)
	if err != nil {
		t.Fatalf("LockfileVersion: %v", err)
	}
	clean.Version = version

	stale := clean
	stale.Members = append([]LockfileMember(nil), clean.Members...)
	stale.Members[0].ContentHash = "publish-time-enrichment"
	if err := CompareLockfiles(stale, clean); err == nil {
		t.Fatal("CompareLockfiles() error = nil, want rejection of committed external content_hash")
	}
}

func TestCompareLockfiles_requiresVersionField(t *testing.T) {
	t.Parallel()
	lock := Lockfile{
		RootChildName: "orchestrator",
		Members: []LockfileMember{{
			ChildName:   "orchestrator",
			Origin:      ClosureMemberOriginVendored,
			Ref:         "./orchestrator.yaml",
			ContentHash: "abc",
		}},
	}
	if err := CompareLockfiles(lock, lock); err == nil {
		t.Fatal("CompareLockfiles() error = nil, want missing version error")
	}
}

func TestLockfilePath(t *testing.T) {
	t.Parallel()
	got := LockfilePath("/tmp/support/bundle.yaml")
	want := filepath.Join("/tmp/support", DefaultLockfileName)
	if got != want {
		t.Fatalf("LockfilePath() = %q, want %q", got, want)
	}
}

func TestReadLockfile_missingFile(t *testing.T) {
	t.Parallel()
	_, err := ReadLockfile(filepath.Join(t.TempDir(), DefaultLockfileName))
	if !os.IsNotExist(err) {
		t.Fatalf("ReadLockfile() error = %v, want IsNotExist", err)
	}
}

func TestParseLockfileJSON_rejectsEmpty(t *testing.T) {
	t.Parallel()
	_, err := ParseLockfileJSON(nil)
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("ParseLockfileJSON() error = %v, want empty lockfile error", err)
	}
}

func TestParseLockfileJSON_rejectsInvalidJSON(t *testing.T) {
	t.Parallel()
	_, err := ParseLockfileJSON([]byte("{"))
	if err == nil || !strings.Contains(err.Error(), "parse lockfile") {
		t.Fatalf("ParseLockfileJSON() error = %v, want parse error", err)
	}
}

func TestValidateLockfileVersion_rejectsMismatch(t *testing.T) {
	t.Parallel()
	lock := Lockfile{
		Version:       "sha256:deadbeef",
		RootChildName: "orchestrator",
		Members: []LockfileMember{{
			ChildName:   "orchestrator",
			Origin:      ClosureMemberOriginVendored,
			Ref:         "./orchestrator.yaml",
			ContentHash: "abc",
		}},
	}
	err := ValidateLockfileVersion(lock)
	if err == nil || !strings.Contains(err.Error(), "does not match body hash") {
		t.Fatalf("ValidateLockfileVersion() error = %v, want mismatch error", err)
	}
}

func TestCompareLockfiles_externalSemverDrift(t *testing.T) {
	t.Parallel()
	committed := Lockfile{
		RootChildName: "orchestrator",
		Members: []LockfileMember{{
			ChildName: "billing",
			Origin:    ClosureMemberOriginExternal,
			Ref:       "support.billing@1.2.0",
			Namespace: "support",
			Name:      "billing",
			Version:   "1.2.0",
		}},
	}
	version, err := LockfileVersion(committed.RootChildName, committed.Members)
	if err != nil {
		t.Fatalf("LockfileVersion: %v", err)
	}
	committed.Version = version

	recomputed := committed
	recomputed.Members = append([]LockfileMember(nil), committed.Members...)
	recomputed.Members[0].Version = "1.3.0"
	if err := CompareLockfiles(committed, recomputed); err == nil {
		t.Fatal("CompareLockfiles() error = nil, want external version drift")
	}
}

func TestCompareLockfiles_rootChildNameDrift(t *testing.T) {
	t.Parallel()
	committed := Lockfile{
		RootChildName: "orchestrator",
		Members: []LockfileMember{{
			ChildName:   "orchestrator",
			Origin:      ClosureMemberOriginVendored,
			Ref:         "./orchestrator.yaml",
			ContentHash: "abc",
		}},
	}
	version, err := LockfileVersion(committed.RootChildName, committed.Members)
	if err != nil {
		t.Fatalf("LockfileVersion: %v", err)
	}
	committed.Version = version

	recomputed := committed
	recomputed.RootChildName = "router"
	if err := CompareLockfiles(committed, recomputed); err == nil {
		t.Fatal("CompareLockfiles() error = nil, want root_child_name drift")
	}
}

func TestCompareLockfiles_memberCountDrift(t *testing.T) {
	t.Parallel()
	committed := Lockfile{
		RootChildName: "orchestrator",
		Members: []LockfileMember{{
			ChildName:   "orchestrator",
			Origin:      ClosureMemberOriginVendored,
			Ref:         "./orchestrator.yaml",
			ContentHash: "abc",
		}},
	}
	version, err := LockfileVersion(committed.RootChildName, committed.Members)
	if err != nil {
		t.Fatalf("LockfileVersion: %v", err)
	}
	committed.Version = version

	recomputed := committed
	recomputed.Members = append(recomputed.Members, LockfileMember{
		ChildName:   "billing",
		Origin:      ClosureMemberOriginVendored,
		Ref:         "./billing.yaml",
		ContentHash: "def",
	})
	if err := CompareLockfiles(committed, recomputed); err == nil {
		t.Fatal("CompareLockfiles() error = nil, want member count drift")
	}
}

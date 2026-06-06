package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/manifest"
)

type resolvedBundle struct {
	Path    string
	Bundle  *manifest.BundleManifest
	Closure *manifest.ClosurePackage
}

type bundleLockState struct {
	resolved *resolvedBundle
	lock     *manifest.Lockfile
	lockRaw  []byte
}

func loadResolvedBundle(bundlePath string) (*resolvedBundle, error) {
	absPath, err := filepath.Abs(bundlePath)
	if err != nil {
		return nil, fmt.Errorf("bundle path: %w", err)
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	bundle, err := manifest.ParseBundle(data)
	if err != nil {
		return nil, err
	}
	bundleRoot := filepath.Dir(absPath)
	closure, err := manifest.WalkBundle(bundleRoot, bundle)
	if err != nil {
		return nil, errors.New(err.Error())
	}
	return &resolvedBundle{
		Path:    absPath,
		Bundle:  bundle,
		Closure: closure,
	}, nil
}

func loadBundleWithLock(bundlePath string) (*bundleLockState, error) {
	resolved, err := loadResolvedBundle(bundlePath)
	if err != nil {
		return nil, err
	}
	lockPath := manifest.LockfilePath(resolved.Path)
	raw, err := os.ReadFile(lockPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &bundleLockState{resolved: resolved}, nil
		}
		return nil, fmt.Errorf("read lockfile: %w", err)
	}
	lock, err := manifest.ParseLockfileJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("parse lockfile: %w", err)
	}
	if err := manifest.ValidateLockfileVersion(*lock); err != nil {
		return nil, fmt.Errorf("bundle.lock.json: %w", err)
	}
	return &bundleLockState{
		resolved: resolved,
		lock:     lock,
		lockRaw:  raw,
	}, nil
}

func (s *bundleLockState) compareLockIfPresent() error {
	if s == nil || s.lock == nil {
		return nil
	}
	recomputed := manifest.LockfileFromClosure(s.resolved.Closure)
	if err := manifest.CompareLockfiles(*s.lock, recomputed); err != nil {
		return fmt.Errorf("bundle.lock.json drift: %w; run phrony bundles lock", err)
	}
	return nil
}

func (r *resolvedBundle) bundleManifestJSON() ([]byte, error) {
	raw, err := json.Marshal(r.Bundle)
	if err != nil {
		return nil, fmt.Errorf("encode bundle manifest: %w", err)
	}
	return raw, nil
}

func closureToMemberPackages(closure *manifest.ClosurePackage) ([]*runtimev1.BundleMemberPackage, error) {
	if closure == nil {
		return nil, fmt.Errorf("bundle closure is nil")
	}
	members := make([]*runtimev1.BundleMemberPackage, 0, len(closure.Members))
	for _, m := range closure.Members {
		pkg := &runtimev1.BundleMemberPackage{
			ChildName:    m.ChildName,
			Origin:       m.Origin,
			AuthoringRef: m.Ref,
			IsRoot:       m.IsRoot,
		}
		switch m.Origin {
		case manifest.ClosureMemberOriginVendored:
			if m.Resolved == nil {
				return nil, fmt.Errorf("vendored member %q is missing resolved manifest", m.ChildName)
			}
			raw, err := m.Resolved.JSON()
			if err != nil {
				return nil, fmt.Errorf("encode vendored member %q: %w", m.ChildName, err)
			}
			pkg.ResolvedManifest = raw
		case manifest.ClosureMemberOriginExternal:
			// Server resolves published agent versions at publish time.
		default:
			return nil, fmt.Errorf("member %q has unsupported origin %q", m.ChildName, m.Origin)
		}
		members = append(members, pkg)
	}
	return members, nil
}

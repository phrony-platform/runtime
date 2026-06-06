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

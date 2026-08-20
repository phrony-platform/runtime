package main

import (
	"context"
	"fmt"
	"os"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/clierr"
	"github.com/phrony-platform/runtime/internal/manifest"
)

type runtimeExternalResolver struct {
	rt runtimev1.RuntimeClient
}

func (r *runtimeExternalResolver) ResolveExternal(ctx context.Context, namespace, name, version string) (string, error) {
	resp, err := r.rt.GetAgentVersion(ctx, &runtimev1.GetAgentVersionRequest{
		AgentRef: &runtimev1.AgentRef{
			Namespace: namespace,
			Name:      name,
			Version:   version,
		},
	})
	if err != nil {
		label := manifest.LogicalID(namespace, name) + "@" + version
		return "", clierr.WrapRPC("get agent version "+label, err)
	}
	return resp.GetContentHash(), nil
}

func closureHasExternalMembers(closure *manifest.ClosurePackage) bool {
	if closure == nil {
		return false
	}
	for _, m := range closure.Members {
		if m.Origin == manifest.ClosureMemberOriginExternal {
			return true
		}
	}
	return false
}

func firstExternalMember(closure *manifest.ClosurePackage) manifest.ClosureMember {
	for _, m := range closure.Members {
		if m.Origin == manifest.ClosureMemberOriginExternal {
			return m
		}
	}
	return manifest.ClosureMember{}
}

func externalMemberRef(m manifest.ClosureMember) string {
	if ref := m.Ref; ref != "" {
		return ref
	}
	version := m.Version
	if version != "" {
		return manifest.LogicalID(m.Namespace, m.Name) + "@" + version
	}
	return manifest.LogicalID(m.Namespace, m.Name)
}

func loadResolvedBundleWithExternals(ctx context.Context, bundlePath, runtimeAddr string) (*resolvedBundle, error) {
	resolved, err := loadResolvedBundle(bundlePath)
	if err != nil {
		return nil, err
	}
	if !closureHasExternalMembers(resolved.Closure) {
		return resolved, nil
	}

	ext := firstExternalMember(resolved.Closure)
	clients, err := openRuntime(ctx, os.Stderr, runtimeAddr)
	if err != nil {
		return nil, fmt.Errorf("bundle has external member %q: %w (set --runtime-addr to resolve content_hash)",
			externalMemberRef(ext), err)
	}
	defer clients.Close()

	resolver := &runtimeExternalResolver{rt: clients.runtime}
	if err := manifest.EnrichExternalMembers(ctx, resolved.Closure, resolver); err != nil {
		return nil, err
	}
	return resolved, nil
}

func loadBundleWithLockAndExternals(ctx context.Context, bundlePath, runtimeAddr string) (*bundleLockState, error) {
	resolved, err := loadResolvedBundleWithExternals(ctx, bundlePath, runtimeAddr)
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

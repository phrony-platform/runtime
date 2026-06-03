package main

import (
	"context"
	"fmt"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/clierr"
	"github.com/phrony-platform/runtime/internal/envfile"
	"github.com/phrony-platform/runtime/internal/manifest"
)

// prepareRunSecrets loads optional env files, then resolves manifest fromEnv values.
func prepareRunSecrets(ctx context.Context, rt runtimev1.RuntimeClient, ref *runtimev1.AgentRef, envFiles []string) (map[string][]byte, error) {
	if err := envfile.ApplyFiles(envFiles); err != nil {
		return nil, err
	}
	return resolveRunSecrets(ctx, rt, ref)
}

// resolveRunSecrets loads the published manifest for ref and reads fromEnv values.
// Returns nil when the agent has no secrets section. ref may omit version; the active
// deployment version is used in that case.
func resolveRunSecrets(ctx context.Context, rt runtimev1.RuntimeClient, ref *runtimev1.AgentRef) (map[string][]byte, error) {
	if ref == nil || ref.GetNamespace() == "" || ref.GetName() == "" {
		return nil, nil
	}

	versionRef := &runtimev1.AgentRef{
		Namespace: ref.GetNamespace(),
		Name:      ref.GetName(),
		Version:   ref.GetVersion(),
	}
	if versionRef.GetVersion() == "" {
		active, err := rt.GetActiveVersion(ctx, &runtimev1.GetActiveVersionRequest{
			AgentRef: &runtimev1.AgentRef{
				Namespace: ref.GetNamespace(),
				Name:      ref.GetName(),
			},
		})
		if err != nil {
			return nil, clierr.WrapRPC("get active version", err)
		}
		versionRef.Version = active.GetVersion()
	}

	resp, err := rt.GetAgentVersion(ctx, &runtimev1.GetAgentVersionRequest{
		AgentRef: versionRef,
	})
	if err != nil {
		return nil, clierr.WrapRPC("get agent version", err)
	}

	agent, err := manifest.ParseJSON(resp.GetManifest())
	if err != nil {
		return nil, fmt.Errorf("parse published manifest: %w", err)
	}
	return manifest.ResolveSecretsFromEnv(agent)
}

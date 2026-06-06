package main

import (
	"context"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/clierr"
	"github.com/phrony-platform/runtime/internal/envfile"
	"github.com/phrony-platform/runtime/internal/manifest"
)

func prepareBundleRunSecrets(ctx context.Context, rt runtimev1.RuntimeClient, bundleRef *runtimev1.BundleRef, envFiles []string) (map[string][]byte, error) {
	if err := envfile.ApplyFiles(envFiles); err != nil {
		return nil, err
	}
	if bundleRef == nil || bundleRef.GetNamespace() == "" || bundleRef.GetName() == "" {
		return nil, nil
	}

	resp, err := rt.GetBundleSecretRequirements(ctx, &runtimev1.GetBundleSecretRequirementsRequest{
		BundleRef: bundleRef,
	})
	if err != nil {
		return nil, clierr.WrapRPC("get bundle secret requirements", err)
	}

	defs := make(map[string]manifest.SecretDefinition, len(resp.GetSecrets()))
	for name, req := range resp.GetSecrets() {
		defs[name] = manifest.SecretDefinition{FromEnv: req.GetFromEnv()}
	}
	return manifest.ResolveSecretsFromDefinitions(defs)
}

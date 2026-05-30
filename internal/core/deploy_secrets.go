package core

import (
	"context"

	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *runtimeServer) persistAgentVersionSecrets(
	ctx context.Context,
	q *store.Queries,
	versionID string,
	agent *manifest.Agent,
	resolved map[string][]byte,
) error {
	if len(agent.Secrets) == 0 {
		if len(resolved) > 0 {
			return status.Error(codes.InvalidArgument, "resolved_secrets provided but manifest has no secrets section")
		}
		return nil
	}
	if s.secretsEnc == nil {
		return status.Error(codes.FailedPrecondition,
			"RUNTIME_SECRETS_ENCRYPTION_KEY must be set on the runtime to deploy agents with secrets")
	}
	if err := validateResolvedSecrets(agent, resolved); err != nil {
		return err
	}
	for name, plaintext := range resolved {
		sealed, err := s.secretsEnc.Encrypt(versionID, name, plaintext)
		if err != nil {
			return status.Errorf(codes.Internal, "encrypt secret %q: %v", name, err)
		}
		if err := q.InsertAgentVersionSecret(ctx, store.InsertAgentVersionSecretParams{
			AgentVersionID: versionID,
			Name:           name,
			KeyVersion:     sealed.KeyVersion,
			Nonce:          sealed.Nonce,
			Ciphertext:     sealed.Ciphertext,
		}); err != nil {
			return status.Errorf(codes.Internal, "persist secret %q: %v", name, err)
		}
	}
	return nil
}

func validateResolvedSecrets(agent *manifest.Agent, resolved map[string][]byte) error {
	for name := range agent.Secrets {
		val, ok := resolved[name]
		if !ok || len(val) == 0 {
			return status.Errorf(codes.InvalidArgument, "missing resolved secret %q", name)
		}
	}
	for name := range resolved {
		if _, ok := agent.Secrets[name]; !ok {
			return status.Errorf(codes.InvalidArgument, "unknown resolved secret %q", name)
		}
	}
	return nil
}

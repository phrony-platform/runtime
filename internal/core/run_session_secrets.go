package core

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"

	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/secrets"
	"github.com/phrony-platform/runtime/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// newSessionRow describes a session to persist, including optional nested-child
// linkage (parent session and delegation depth) for agent delegation.
type newSessionRow struct {
	sessionID        string
	agentVersionID   string
	bundleVersionID  *string
	input            []byte
	resolved         map[string][]byte
	parentSessionID  *string
	depth            int
}

// createRunSession validates resolved secrets, inserts a top-level session row,
// and persists encrypted session secrets in a single transaction.
func (s *runtimeServer) createRunSession(
	ctx context.Context,
	agentVersionID string,
	bundleVersionID *string,
	inputJSON []byte,
	resolved map[string][]byte,
) (string, error) {
	return s.persistNewSession(ctx, newSessionRow{
		sessionID:       newRunSessionID(),
		agentVersionID:  agentVersionID,
		bundleVersionID: bundleVersionID,
		input:           inputJSON,
		resolved:        resolved,
	})
}

// createChildRunSession persists a nested child session (agent delegation) with
// an explicit id and parent linkage, reusing the same transactional insert as a
// top-level run so secret validation and persistence stay identical.
func (s *runtimeServer) createChildRunSession(
	ctx context.Context,
	sessionID, parentSessionID, agentVersionID string,
	inputJSON []byte,
	resolved map[string][]byte,
	depth int,
) (string, error) {
	parent := parentSessionID
	return s.persistNewSession(ctx, newSessionRow{
		sessionID:       sessionID,
		agentVersionID:  agentVersionID,
		input:           inputJSON,
		resolved:        resolved,
		parentSessionID: &parent,
		depth:           depth,
	})
}

// persistNewSession inserts a running session row and its encrypted secrets in a
// single transaction. It is the shared core for top-level and nested child runs.
func (s *runtimeServer) persistNewSession(ctx context.Context, row newSessionRow) (string, error) {
	if s.db == nil {
		return "", status.Error(codes.FailedPrecondition, "database is not configured")
	}

	q := store.New(s.db.DB)
	agent, err := loadAgentManifestForVersion(ctx, q, row.agentVersionID)
	if err != nil {
		return "", err
	}

	var bundleUnion map[string]manifest.SecretDefinition
	if row.bundleVersionID != nil && row.depth == 0 {
		bundleUnion, _, err = loadBundleSecretUnion(ctx, q, *row.bundleVersionID)
		if err != nil {
			return "", err
		}
	}

	tx, err := s.db.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", status.Errorf(codes.Internal, "begin transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	txQ := q.WithTx(tx)

	if _, err := txQ.InsertSession(ctx, store.InsertSessionParams{
		ID:              row.sessionID,
		AgentVersionID:  row.agentVersionID,
		BundleVersionID: row.bundleVersionID,
		Input:           row.input,
		Status:          model.SessionStatusRunning,
		ParentSessionID: row.parentSessionID,
		Depth:           row.depth,
	}); err != nil {
		return "", status.Errorf(codes.Internal, "persist session: %v", err)
	}

	if err := s.persistSessionSecrets(ctx, txQ, row.sessionID, agent, bundleUnion, row.resolved); err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", status.Errorf(codes.Internal, "commit transaction: %v", err)
	}
	return row.sessionID, nil
}

func loadAgentManifestForVersion(ctx context.Context, q *store.Queries, agentVersionID string) (*manifest.Agent, error) {
	raw, err := q.GetAgentVersionManifest(ctx, agentVersionID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, status.Errorf(codes.NotFound, "agent version %q not found", agentVersionID)
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "load manifest: %v", err)
	}
	agent, err := manifest.ParseJSON(raw)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "parse manifest: %v", err)
	}
	return agent, nil
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

func validateBundleRunSecrets(union map[string]manifest.SecretDefinition, resolved map[string][]byte) error {
	for name := range union {
		val, ok := resolved[name]
		if !ok || len(val) == 0 {
			return status.Errorf(codes.InvalidArgument, "missing resolved secret %q", name)
		}
	}
	for name := range resolved {
		if _, ok := union[name]; !ok {
			return status.Errorf(codes.InvalidArgument, "unknown resolved secret %q", name)
		}
	}
	return nil
}

func (s *runtimeServer) persistSessionSecrets(
	ctx context.Context,
	q *store.Queries,
	sessionID string,
	agent *manifest.Agent,
	bundleUnion map[string]manifest.SecretDefinition,
	resolved map[string][]byte,
) error {
	required := agent.Secrets
	if len(bundleUnion) > 0 {
		required = bundleUnion
	}
	if len(required) == 0 {
		if len(resolved) > 0 {
			return status.Error(codes.InvalidArgument, "resolved_secrets provided but manifest has no secrets section")
		}
		return nil
	}
	if s.secretsEnc == nil {
		return status.Error(codes.FailedPrecondition,
			"RUNTIME_SECRETS_ENCRYPTION_KEY must be set on the runtime to run agents with secrets")
	}
	if len(bundleUnion) > 0 {
		if err := validateBundleRunSecrets(bundleUnion, resolved); err != nil {
			return err
		}
	} else if err := validateResolvedSecrets(agent, resolved); err != nil {
		return err
	}
	if err := s.secretsEnc.PersistSessionSecrets(ctx, q, sessionID, resolved); err != nil {
		return status.Errorf(codes.Internal, "persist session secrets: %v", err)
	}
	return nil
}

// finalizeSessionSecrets deletes encrypted session secrets after a terminal
// transition. Errors are logged but not returned so purge never blocks status updates.
func (s *runtimeServer) finalizeSessionSecrets(ctx context.Context, q *store.Queries, sessionID string) {
	if err := secrets.PurgeSessionSecrets(ctx, q, sessionID); err != nil {
		slog.Warn("purge session secrets", "session_id", sessionID, "error", err)
	}
}

// purgeOrphanedTerminalSessionSecrets removes session_secrets rows left behind when
// a process crashed between marking a session terminal and purging secrets.
func (s *runtimeServer) purgeOrphanedTerminalSessionSecrets(ctx context.Context) {
	q, err := s.queries()
	if err != nil {
		return
	}
	if err := q.DeleteTerminalSessionSecrets(ctx); err != nil {
		slog.Warn("purge orphaned terminal session secrets", "error", err)
	}
}

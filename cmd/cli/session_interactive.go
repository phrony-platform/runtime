package main

import (
	"context"
	"os"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/clierr"
	"github.com/spf13/cobra"
)

func runInteractiveSessionCLI(
	cmd *cobra.Command,
	runtimeAddr *string,
	start *runtimev1.RunSessionInteractiveStart,
	noTUI bool,
) error {
	if noTUI {
		_ = os.Setenv("PHRONY_NO_TUI", "1")
	}

	return withRuntimeClient(cmd, *runtimeAddr, func(rt runtimev1.RuntimeClient) error {
		if !isInteractiveAttachReplay(start) && start.GetAgentRef() != nil {
			resolvedSecrets, err := resolveRunSecrets(cmd.Context(), rt, start.GetAgentRef())
			if err != nil {
				return err
			}
			if len(resolvedSecrets) > 0 {
				start.ResolvedSecrets = resolvedSecrets
			}
		}

		stream, err := rt.RunSessionInteractive(cmd.Context())
		if err != nil {
			return clierr.WrapRPC("run session", err)
		}

		controls := &sessionControls{
			cancel: func(ctx context.Context, sessionID string) error {
				if _, err := rt.CancelSession(ctx, &runtimev1.CancelSessionRequest{
					SessionId: sessionID,
				}); err != nil {
					return clierr.WrapRPC("cancel session", err)
				}
				return nil
			},
			complete: func(ctx context.Context, sessionID string) error {
				if _, err := rt.CompleteSession(ctx, &runtimev1.CompleteSessionRequest{
					SessionId: sessionID,
				}); err != nil {
					return clierr.WrapRPC("complete session", err)
				}
				return nil
			},
		}

		return runInteractiveSession(
			cmd.Context(),
			stream,
			start,
			cmd.InOrStdin(),
			cmd.OutOrStdout(),
			cmd.ErrOrStderr(),
			controls,
			rt,
		)
	})
}

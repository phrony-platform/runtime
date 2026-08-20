package main

import (
	"encoding/json"
	"fmt"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/agentref"
	"github.com/phrony-platform/runtime/internal/clierr"
	"github.com/phrony-platform/runtime/internal/cliout"
	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/spf13/cobra"
)

func newBundlesCommand(runtimeAddr *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bundles",
		Short: "Validate, publish, and deploy multi-agent bundles",
	}

	cmd.AddCommand(
		newBundlesLsCommand(runtimeAddr),
		newBundleLockCommand(runtimeAddr),
		newBundleValidateCommand(runtimeAddr),
		newBundlePublishCommand(runtimeAddr),
		newBundleDeployCommand(runtimeAddr),
		newBundleVersionsCommand(runtimeAddr),
		newBundleActiveCommand(runtimeAddr),
		newBundleHistoryCommand(runtimeAddr),
		newBundleSecretRequirementsCommand(runtimeAddr),
		newBundleRunCommand(runtimeAddr),
	)
	return cmd
}

func newBundlesLsCommand(runtimeAddr *string) *cobra.Command {
	var namespace string

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List bundles",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runBundlesList(cmd, runtimeAddr, namespace)
		},
	}
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "filter by namespace")
	return cmd
}

func runBundlesList(cmd *cobra.Command, runtimeAddr *string, namespace string) error {
	return withRuntimeClient(cmd, *runtimeAddr, func(rt runtimev1.RuntimeClient) error {
		resp, err := rt.ListBundles(cmd.Context(), &runtimev1.ListBundlesRequest{
			Namespace: namespace,
		})
		if err != nil {
			return clierr.WrapRPC("list bundles", err)
		}

		headers := []string{"ID", "NAMESPACE", "NAME", "OWNER"}
		rows := make([][]string, 0, len(resp.GetBundles()))
		for _, b := range resp.GetBundles() {
			rows = append(rows, []string{
				b.GetId(),
				b.GetNamespace(),
				b.GetName(),
				b.GetOwner(),
			})
		}
		return cliout.WriteTable(cmd.OutOrStdout(), headers, rows)
	})
}

func newBundleLockCommand(runtimeAddr *string) *cobra.Command {
	return &cobra.Command{
		Use:   "lock BUNDLE",
		Short: "Write bundle.lock.json from the current closure",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBundleLock(cmd, runtimeAddr, args[0])
		},
	}
}

func newBundleValidateCommand(runtimeAddr *string) *cobra.Command {
	var requireLock bool
	cmd := &cobra.Command{
		Use:   "validate BUNDLE",
		Short: "Validate a bundle manifest and closure locally (no publish)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBundleValidate(cmd, runtimeAddr, args[0], requireLock)
		},
	}
	cmd.Flags().BoolVar(&requireLock, "require-lock", false, "fail when bundle.lock.json is missing (CI gate)")
	return cmd
}

func newBundlePublishCommand(runtimeAddr *string) *cobra.Command {
	return &cobra.Command{
		Use:   "publish BUNDLE",
		Short: "Publish an immutable bundle version to the runtime",
		Long:  "Verify bundle.lock.json against the closure and publish all vendored members. Use bundle deploy to activate a published semver or lock hash for sessions.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBundlePublish(cmd, runtimeAddr, args[0])
		},
	}
}

func newBundleDeployCommand(runtimeAddr *string) *cobra.Command {
	return &cobra.Command{
		Use:   "deploy BUNDLE@VERSION",
		Short: "Activate a published bundle version",
		Long:  "Record a deployment so the given semver or lock hash becomes the active bundle version for sessions in this runtime.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBundleDeploy(cmd, runtimeAddr, args[0])
		},
	}
}

func newBundleVersionsCommand(runtimeAddr *string) *cobra.Command {
	return &cobra.Command{
		Use:   "versions BUNDLE",
		Short: "List published versions for a bundle",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBundleVersionsList(cmd, runtimeAddr, args[0])
		},
	}
}

func newBundleActiveCommand(runtimeAddr *string) *cobra.Command {
	return &cobra.Command{
		Use:   "active BUNDLE",
		Short: "Show the active deployed version for a bundle",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBundleActive(cmd, runtimeAddr, args[0])
		},
	}
}

func newBundleHistoryCommand(runtimeAddr *string) *cobra.Command {
	return &cobra.Command{
		Use:   "history BUNDLE",
		Short: "List deployment history for a bundle",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBundleHistory(cmd, runtimeAddr, args[0])
		},
	}
}

func newBundleRunCommand(runtimeAddr *string) *cobra.Command {
	var input string
	var attach bool
	var envFiles []string

	cmd := &cobra.Command{
		Use:   "run BUNDLE[@VERSION]",
		Short: "Start a session for a deployed bundle",
		Long: "Start a new session for BUNDLE (namespace/name). Uses the active bundle deployment when @version is omitted. " +
			"By default the runtime runs the first turn in the background and the CLI prints the session id and exits. " +
			"Use --attach to start the session in the background and attach a foreground view (Ctrl+C detaches; use sessions cancel to stop).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBundleSession(cmd, runtimeAddr, args[0], input, attach, envFiles)
		},
	}
	cmd.Flags().StringVar(&input, "input", "", "session input as a JSON object")
	cmd.Flags().BoolVarP(&attach, "attach", "a", false, "start in the background and attach an interactive view")
	cmd.Flags().StringArrayVarP(&envFiles, "env-file", "e", nil, "load environment variables from a file before resolving secrets (repeatable; does not override existing env)")
	return cmd
}

func runBundleLock(cmd *cobra.Command, runtimeAddr *string, bundlePath string) error {
	resolved, err := loadResolvedBundleWithExternals(cmd.Context(), bundlePath, *runtimeAddr)
	if err != nil {
		return err
	}
	lock := manifest.LockfileFromClosure(resolved.Closure)
	lockPath := manifest.LockfilePath(resolved.Path)
	if err := manifest.WriteLockfile(lockPath, lock); err != nil {
		return err
	}
	if _, err := manifest.UnionBundleSecrets(resolved.Closure.Members); err != nil {
		return err
	}
	bundle := resolved.Bundle
	fmt.Fprintf(cmd.OutOrStdout(), "locked: %s %s\n",
		agentref.Format(bundle.Metadata.Namespace, bundle.Metadata.Name),
		resolved.Closure.Version,
	)
	fmt.Fprintf(cmd.OutOrStdout(), "members: %d (root: %s)\n",
		len(resolved.Closure.Members),
		resolved.Closure.RootChildName,
	)
	return nil
}

func runBundleValidate(cmd *cobra.Command, runtimeAddr *string, bundlePath string, requireLock bool) error {
	state, err := loadBundleWithLockAndExternals(cmd.Context(), bundlePath, *runtimeAddr)
	if err != nil {
		return err
	}
	if requireLock && state.lock == nil {
		return fmt.Errorf("no committed lock; run phrony bundles lock")
	}
	if err := state.compareLockIfPresent(); err != nil {
		return err
	}

	resolved := state.resolved
	if _, err := manifest.UnionBundleSecrets(resolved.Closure.Members); err != nil {
		return err
	}
	bundle := resolved.Bundle
	for _, member := range resolved.Closure.Members {
		if member.Origin != manifest.ClosureMemberOriginVendored || member.Resolved == nil {
			continue
		}
		for _, msg := range manifest.UnsetSecretEnvVars(member.Resolved.Agent) {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s (%s): %s is not set in the local environment\n",
				member.ChildName, member.Ref, msg)
		}
	}

	fmt.Fprintf(cmd.OutOrStdout(), "valid: %s %s\n",
		agentref.Format(bundle.Metadata.Namespace, bundle.Metadata.Name),
		resolved.Closure.Version,
	)
	fmt.Fprintf(cmd.OutOrStdout(), "members: %d (root: %s)\n",
		len(resolved.Closure.Members),
		resolved.Closure.RootChildName,
	)
	return nil
}

func runBundlePublish(cmd *cobra.Command, runtimeAddr *string, bundlePath string) error {
	state, err := loadBundleWithLockAndExternals(cmd.Context(), bundlePath, *runtimeAddr)
	if err != nil {
		return err
	}
	if state.lock == nil {
		return fmt.Errorf("no committed lock; run phrony bundles lock")
	}
	if err := state.compareLockIfPresent(); err != nil {
		return err
	}

	resolved := state.resolved
	bundleJSON, err := resolved.bundleManifestJSON()
	if err != nil {
		return err
	}
	members, err := closureToMemberPackages(resolved.Closure)
	if err != nil {
		return err
	}

	clients, err := openRuntime(cmd.Context(), cmd.ErrOrStderr(), *runtimeAddr)
	if err != nil {
		return err
	}
	defer clients.Close()

	resp, err := clients.runtime.PublishBundle(cmd.Context(), &runtimev1.PublishBundleRequest{
		BundleManifest: bundleJSON,
		Members:        members,
		Actor:          cliActor(),
		CommittedLock:  state.lockRaw,
	})
	if err != nil {
		return clierr.WrapRPC("publish bundle", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "%s %s (%s)\n",
		agentref.Format(resp.GetNamespace(), resp.GetName()),
		resp.GetVersion(),
		resp.GetLockHash(),
	)
	return nil
}

func runBundleVersionsList(cmd *cobra.Command, runtimeAddr *string, bundleName string) error {
	ref, err := parseBundleRef(bundleName)
	if err != nil {
		return err
	}

	return withRuntimeClient(cmd, *runtimeAddr, func(rt runtimev1.RuntimeClient) error {
		resp, err := rt.ListBundleVersions(cmd.Context(), &runtimev1.ListBundleVersionsRequest{
			BundleRef: ref,
		})
		if err != nil {
			return clierr.WrapRPC("list bundle versions", err)
		}

		headers := []string{"VERSION", "LOCK_HASH", "ID", "PUBLISHED_AT"}
		rows := make([][]string, 0, len(resp.GetVersions()))
		for _, v := range resp.GetVersions() {
			rows = append(rows, []string{
				v.GetVersion(),
				v.GetLockHash(),
				v.GetId(),
				v.GetPublishedAt(),
			})
		}
		return cliout.WriteTable(cmd.OutOrStdout(), headers, rows)
	})
}

func runBundleActive(cmd *cobra.Command, runtimeAddr *string, bundleName string) error {
	ref, err := parseBundleRef(bundleName)
	if err != nil {
		return err
	}

	return withRuntimeClient(cmd, *runtimeAddr, func(rt runtimev1.RuntimeClient) error {
		resp, err := rt.GetActiveBundle(cmd.Context(), &runtimev1.GetActiveBundleRequest{
			BundleRef: ref,
		})
		if err != nil {
			return clierr.WrapRPC("get active bundle", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "%s@%s (%s)",
			agentref.Format(ref.GetNamespace(), ref.GetName()),
			resp.GetVersion(),
			resp.GetLockHash(),
		)
		if at := resp.GetDeployedAt(); at != "" {
			fmt.Fprintf(cmd.OutOrStdout(), " deployed %s", at)
		}
		if actor := resp.GetActor(); actor != "" {
			fmt.Fprintf(cmd.OutOrStdout(), " by %s", actor)
		}
		fmt.Fprintln(cmd.OutOrStdout())
		return nil
	})
}

func runBundleHistory(cmd *cobra.Command, runtimeAddr *string, bundleName string) error {
	ref, err := parseBundleRef(bundleName)
	if err != nil {
		return err
	}

	return withRuntimeClient(cmd, *runtimeAddr, func(rt runtimev1.RuntimeClient) error {
		resp, err := rt.ListBundleDeployments(cmd.Context(), &runtimev1.ListBundleDeploymentsRequest{
			BundleRef: ref,
		})
		if err != nil {
			return clierr.WrapRPC("list bundle deployments", err)
		}

		headers := []string{"VERSION", "LOCK_HASH", "ACTION", "ACTOR", "CREATED_AT"}
		rows := make([][]string, 0, len(resp.GetDeployments()))
		for _, d := range resp.GetDeployments() {
			rows = append(rows, []string{
				d.GetVersion(),
				d.GetLockHash(),
				d.GetAction(),
				d.GetActor(),
				d.GetCreatedAt(),
			})
		}
		return cliout.WriteTable(cmd.OutOrStdout(), headers, rows)
	})
}

func runBundleDeploy(cmd *cobra.Command, runtimeAddr *string, bundleRef string) error {
	ref, err := parseBundleRefVersionRequired(bundleRef)
	if err != nil {
		return err
	}

	return withRuntimeClient(cmd, *runtimeAddr, func(rt runtimev1.RuntimeClient) error {
		resp, err := rt.DeployBundle(cmd.Context(), &runtimev1.DeployBundleRequest{
			BundleRef: ref,
			Actor:     cliActor(),
		})
		if err != nil {
			return clierr.WrapRPC("deploy bundle", err)
		}

		line := fmt.Sprintf("deployed %s@%s (%s)",
			agentref.Format(resp.GetNamespace(), resp.GetName()),
			resp.GetVersion(),
			resp.GetLockHash(),
		)
		if prev := resp.GetPreviousVersion(); prev != "" {
			line += fmt.Sprintf(" (previous: %s", prev)
			if prevHash := resp.GetPreviousLockHash(); prevHash != "" {
				line += fmt.Sprintf(", %s", prevHash)
			}
			line += ")"
		}
		if at := resp.GetDeployedAt(); at != "" {
			line += fmt.Sprintf(" at %s", at)
		}
		fmt.Fprintln(cmd.OutOrStdout(), line)
		return nil
	})
}

func runBundleSession(cmd *cobra.Command, runtimeAddr *string, bundleRefArg, input string, attach bool, envFiles []string) error {
	ref, err := parseBundleRef(bundleRefArg)
	if err != nil {
		return err
	}

	var inputBytes []byte
	if input != "" {
		if !json.Valid([]byte(input)) {
			return fmt.Errorf("input must be valid JSON")
		}
		inputBytes = []byte(input)
	}

	if attach {
		return runAttachedBundleSession(cmd, runtimeAddr, ref, inputBytes, envFiles)
	}
	return runDetachedBundleSession(cmd, runtimeAddr, ref, inputBytes, envFiles)
}

func runAttachedBundleSession(cmd *cobra.Command, runtimeAddr *string, ref *runtimev1.BundleRef, input []byte, envFiles []string) error {
	return runAttachedSessionWithStart(cmd, runtimeAddr, "run bundle session", func(rt runtimev1.RuntimeClient) (*runtimev1.RunSessionRequest, error) {
		resolvedSecrets, err := prepareBundleRunSecrets(cmd.Context(), rt, ref, envFiles)
		if err != nil {
			return nil, err
		}
		return &runtimev1.RunSessionRequest{
			BundleRef:       ref,
			Input:           input,
			ResolvedSecrets: resolvedSecrets,
		}, nil
	})
}

func runDetachedBundleSession(cmd *cobra.Command, runtimeAddr *string, ref *runtimev1.BundleRef, input []byte, envFiles []string) error {
	return withRuntimeClient(cmd, *runtimeAddr, func(rt runtimev1.RuntimeClient) error {
		resolvedSecrets, err := prepareBundleRunSecrets(cmd.Context(), rt, ref, envFiles)
		if err != nil {
			return err
		}
		resp, err := rt.RunSession(cmd.Context(), &runtimev1.RunSessionRequest{
			BundleRef:       ref,
			Input:           input,
			ResolvedSecrets: resolvedSecrets,
		})
		if err != nil {
			return clierr.WrapRPC("run bundle session", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "session %s started (status: %s)\n",
			resp.GetSessionId(),
			resp.GetStatus(),
		)
		return nil
	})
}

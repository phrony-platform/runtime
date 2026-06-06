package main

import (
	"encoding/json"
	"fmt"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/agentref"
	"github.com/phrony-platform/runtime/internal/clierr"
	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/spf13/cobra"
)

func newBundlesCommand(runtimeAddr *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bundle",
		Short: "Validate, publish, and deploy multi-agent bundles",
	}

	cmd.AddCommand(
		newBundleValidateCommand(),
		newBundlePublishCommand(runtimeAddr),
		newBundleDeployCommand(runtimeAddr),
		newBundleRunCommand(runtimeAddr),
	)
	return cmd
}

func newBundleValidateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "validate BUNDLE",
		Short: "Validate a bundle manifest and closure locally (no publish)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBundleValidate(cmd, args[0])
		},
	}
}

func newBundlePublishCommand(runtimeAddr *string) *cobra.Command {
	return &cobra.Command{
		Use:   "publish BUNDLE",
		Short: "Publish an immutable bundle version to the runtime",
		Long:  "Walk the bundle closure, build the lockfile, and publish all vendored members. Use bundle deploy to activate a published lock hash for sessions.",
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
		Long:  "Record a deployment so the given lock hash becomes the active bundle version for sessions in this runtime.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBundleDeploy(cmd, runtimeAddr, args[0])
		},
	}
}

func newBundleRunCommand(runtimeAddr *string) *cobra.Command {
	var input, fromBundle string
	var envFiles []string

	cmd := &cobra.Command{
		Use:   "run BUNDLE[@VERSION]",
		Short: "Start a session for a deployed bundle",
		Long:  "Start a new session for BUNDLE (namespace/name). Uses the active bundle deployment when @version is omitted.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBundleSession(cmd, runtimeAddr, args[0], fromBundle, input, envFiles)
		},
	}
	cmd.Flags().StringVar(&fromBundle, "from", "", "bundle manifest path for resolving local fromEnv secrets on the root member")
	cmd.Flags().StringVar(&input, "input", "", "session input as a JSON object")
	cmd.Flags().StringArrayVarP(&envFiles, "env-file", "e", nil, "load environment variables from a file before resolving secrets (repeatable; does not override existing env)")
	return cmd
}

func runBundleValidate(cmd *cobra.Command, bundlePath string) error {
	resolved, err := loadResolvedBundle(bundlePath)
	if err != nil {
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
	resolved, err := loadResolvedBundle(bundlePath)
	if err != nil {
		return err
	}
	bundleJSON, err := resolved.bundleManifestJSON()
	if err != nil {
		return err
	}
	members, err := closureToMemberPackages(resolved.Closure)
	if err != nil {
		return err
	}

	clients, err := dialRuntime(cmd.Context(), *runtimeAddr)
	if err != nil {
		return err
	}
	defer clients.Close()

	resp, err := clients.runtime.PublishBundle(cmd.Context(), &runtimev1.PublishBundleRequest{
		BundleManifest: bundleJSON,
		Members:        members,
		Actor:          cliActor(),
	})
	if err != nil {
		return clierr.WrapRPC("publish bundle", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n",
		agentref.Format(resp.GetNamespace(), resp.GetName()),
		resp.GetLockHash(),
	)
	return nil
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

		line := fmt.Sprintf("deployed %s@%s",
			agentref.Format(resp.GetNamespace(), resp.GetName()),
			resp.GetVersion(),
		)
		if prev := resp.GetPreviousVersion(); prev != "" {
			line += fmt.Sprintf(" (previous: %s)", prev)
		}
		if at := resp.GetDeployedAt(); at != "" {
			line += fmt.Sprintf(" at %s", at)
		}
		fmt.Fprintln(cmd.OutOrStdout(), line)
		return nil
	})
}

func runBundleSession(cmd *cobra.Command, runtimeAddr *string, bundleRefArg, fromBundle, input string, envFiles []string) error {
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

	return withRuntimeClient(cmd, *runtimeAddr, func(rt runtimev1.RuntimeClient) error {
		resolvedSecrets, err := prepareBundleRunSecrets(fromBundle, envFiles)
		if err != nil {
			return err
		}
		resp, err := rt.RunSession(cmd.Context(), &runtimev1.RunSessionRequest{
			BundleRef:       ref,
			Input:           inputBytes,
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

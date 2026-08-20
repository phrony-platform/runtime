package main

import (
	"fmt"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/agentref"
	"github.com/phrony-platform/runtime/internal/clierr"
	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/spf13/cobra"
)

func newAgentPublishCommand(runtimeAddr *string) *cobra.Command {
	return &cobra.Command{
		Use:   "publish MANIFEST",
		Short: "Publish an agent manifest to the runtime",
		Long:  "Validate and publish an immutable agent version. Use agents deploy to activate a published version for sessions.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgentPublish(cmd, runtimeAddr, args[0])
		},
	}
}

func runAgentPublish(cmd *cobra.Command, runtimeAddr *string, manifestPath string) error {
	kind, err := readManifestKind(manifestPath)
	if err != nil {
		return err
	}
	if kind == manifest.KindBundle {
		return fmt.Errorf("bundle manifest: use phrony bundles publish instead")
	}

	resolved, err := loadResolvedManifest(manifestPath)
	if err != nil {
		return err
	}
	manifestJSON, err := resolved.JSON()
	if err != nil {
		return fmt.Errorf("encode resolved manifest: %w", err)
	}

	clients, err := openRuntime(cmd.Context(), cmd.ErrOrStderr(), *runtimeAddr)
	if err != nil {
		return err
	}
	defer clients.Close()

	resp, err := clients.runtime.Publish(cmd.Context(), &runtimev1.PublishRequest{
		Manifest: manifestJSON,
		Actor:    cliActor(),
	})
	if err != nil {
		return clierr.WrapRPC("publish", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n",
		agentref.Format(resp.GetNamespace(), resp.GetName()),
		resp.GetVersion(),
	)
	return nil
}

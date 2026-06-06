package main

import (
	"sort"
	"strings"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/clierr"
	"github.com/phrony-platform/runtime/internal/cliout"
	"github.com/spf13/cobra"
)

func newBundleSecretRequirementsCommand(runtimeAddr *string) *cobra.Command {
	return &cobra.Command{
		Use:   "secret-requirements BUNDLE[@VERSION]",
		Short: "List secret requirements for a deployed bundle",
		Long:  "Inspect the union of fromEnv secret requirements across all frozen closure members. Uses the active bundle deployment when @version is omitted.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBundleSecretRequirements(cmd, runtimeAddr, args[0])
		},
	}
}

func runBundleSecretRequirements(cmd *cobra.Command, runtimeAddr *string, bundleRefArg string) error {
	ref, err := parseBundleRef(bundleRefArg)
	if err != nil {
		return err
	}
	return withRuntimeClient(cmd, *runtimeAddr, func(rt runtimev1.RuntimeClient) error {
		resp, err := rt.GetBundleSecretRequirements(cmd.Context(), &runtimev1.GetBundleSecretRequirementsRequest{
			BundleRef: ref,
		})
		if err != nil {
			return clierr.WrapRPC("get bundle secret requirements", err)
		}

		secrets := resp.GetSecrets()
		names := make([]string, 0, len(secrets))
		for name := range secrets {
			names = append(names, name)
		}
		sort.Strings(names)

		headers := []string{"SECRET_NAME", "FROM_ENV", "DECLARED_BY"}
		rows := make([][]string, 0, len(names))
		for _, name := range names {
			req := secrets[name]
			rows = append(rows, []string{
				name,
				req.GetFromEnv(),
				joinDeclaredBy(req.GetDeclaredBy()),
			})
		}
		return cliout.WriteTable(cmd.OutOrStdout(), headers, rows)
	})
}

func joinDeclaredBy(members []string) string {
	if len(members) == 0 {
		return ""
	}
	out := append([]string(nil), members...)
	sort.Strings(out)
	return strings.Join(out, ", ")
}

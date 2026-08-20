package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/phrony-platform/runtime/internal/cliupgrade"
	"github.com/phrony-platform/runtime/internal/version"
	"github.com/spf13/cobra"
)

var (
	testUpgradeLatestVersion func(ctx context.Context, client *http.Client) (string, error)
	testUpgradeIsDevBuild    func() bool
)

func newUpgradeCommand() *cobra.Command {
	var (
		checkOnly     bool
		targetVersion string
		yes           bool
	)
	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade the phrony CLI to the latest release",
		Long:  "Fetch the latest release from the Go module proxy and install it with go install. Requires the Go toolchain on PATH.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpgrade(cmd, upgradeOptions{
				checkOnly:     checkOnly,
				targetVersion: targetVersion,
				yes:           yes,
			})
		},
	}
	cmd.Flags().BoolVar(&checkOnly, "check", false, "report whether an update is available and exit")
	cmd.Flags().StringVar(&targetVersion, "version", "", "install a specific release tag (for example v0.3.0) instead of @latest")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation before installing")
	return cmd
}

type upgradeOptions struct {
	checkOnly     bool
	targetVersion string
	yes           bool
}

func runUpgrade(cmd *cobra.Command, opts upgradeOptions) error {
	isDevBuild := cliupgrade.IsDevBuild
	if testUpgradeIsDevBuild != nil {
		isDevBuild = testUpgradeIsDevBuild
	}
	if isDevBuild() {
		fmt.Fprintln(cmd.ErrOrStderr(), "phrony upgrade only applies to installed binaries (not go run / test builds).")
		fmt.Fprintln(cmd.ErrOrStderr(), "For local development, run: make install-cli")
		return fmt.Errorf("upgrade unavailable for dev build")
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	current := version.CLIVersion
	installVersion := strings.TrimSpace(opts.targetVersion)
	if installVersion != "" {
		installVersion = cliupgrade.StripVPrefix(installVersion)
	}

	var latest string
	var err error
	if installVersion == "" {
		latestVersion := cliupgrade.LatestVersion
		if testUpgradeLatestVersion != nil {
			latestVersion = testUpgradeLatestVersion
		}
		latest, err = latestVersion(ctx, http.DefaultClient)
		if err != nil {
			return fmt.Errorf("check for updates: %w", err)
		}
		installVersion = latest
	}

	if opts.checkOnly {
		if !cliupgrade.NeedsUpgrade(current, installVersion) {
			fmt.Fprintf(cmd.OutOrStdout(), "phrony CLI is up to date (v%s)\n", current)
			return nil
		}
		fmt.Fprintf(cmd.OutOrStdout(), "update available: v%s → v%s\n", current, installVersion)
		return nil
	}

	if !cliupgrade.NeedsUpgrade(current, installVersion) && opts.targetVersion == "" {
		fmt.Fprintf(cmd.OutOrStdout(), "phrony CLI is up to date (v%s)\n", current)
		return nil
	}

	if !opts.yes {
		prompt := fmt.Sprintf("Upgrade to v%s? [y/N] ", installVersion)
		if _, err := fmt.Fprint(cmd.OutOrStdout(), prompt); err != nil {
			return err
		}
		answer, err := readConfirmation(cmd.InOrStdin())
		if err != nil {
			return err
		}
		if !answer {
			fmt.Fprintln(cmd.OutOrStdout(), "upgrade cancelled")
			return nil
		}
	}

	if err := cliupgrade.Install(ctx, cliupgrade.InstallOptions{Version: installVersion}); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "upgraded phrony CLI v%s → v%s\n", current, installVersion)
	return nil
}

func readConfirmation(r io.Reader) (bool, error) {
	scanner := bufio.NewScanner(r)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return false, err
		}
		return false, nil
	}
	line := strings.TrimSpace(strings.ToLower(scanner.Text()))
	return line == "y" || line == "yes", nil
}

package main

import (
	"fmt"

	"github.com/phrony-platform/runtime/internal/telemetry"
	"github.com/spf13/cobra"
)

func newTelemetryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "telemetry",
		Short: "Manage telemetry preferences",
	}
	cmd.AddCommand(
		newTelemetryStatusCommand(),
		newTelemetryEnableCommand(),
		newTelemetryDisableCommand(),
	)
	return cmd
}

func newTelemetryStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show telemetry status",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTelemetryStatus(cmd)
		},
	}
}

func newTelemetryEnableCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "enable",
		Short: "Enable telemetry in the local config file",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTelemetryEnable(cmd)
		},
	}
}

func newTelemetryDisableCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "disable",
		Short: "Disable telemetry in the local config file",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTelemetryDisable(cmd)
		},
	}
}

func runTelemetryStatus(cmd *cobra.Command) error {
	st, err := telemetry.CurrentStatus()
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(),
		"install_id: %s\nconfig_enabled: %t\nenv_disabled: %t\neffective_enabled: %t\n",
		st.InstallID, st.ConfigEnabled, st.EnvDisabled, st.EffectiveEnabled,
	)
	return err
}

func runTelemetryEnable(cmd *cobra.Command) error {
	if err := telemetry.Enable(); err != nil {
		return err
	}
	_, err := fmt.Fprintln(cmd.OutOrStdout(), "Telemetry enabled in config (env opt-out vars still apply).")
	return err
}

func runTelemetryDisable(cmd *cobra.Command) error {
	if err := telemetry.Disable(); err != nil {
		return err
	}
	_, err := fmt.Fprintln(cmd.OutOrStdout(), "Telemetry disabled in config.")
	return err
}

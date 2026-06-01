package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

const initAgentManifestYAML = `apiVersion: phrony.com/v1
kind: Agent

metadata:
  name: my-agent
  namespace: default
  version: 0.1.0

spec:
  purpose: Describe what this agent does.
  instructions:
    text: |
      You are a helpful assistant.
  model:
    provider: anthropic
    name: claude-sonnet-4-5

output:
  format: text
`

func newInitCommand() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "init [DIR]",
		Short: "Scaffold a new agent.yaml",
		Long:  "Write a minimal agent.yaml in DIR (default: current directory).",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				dir = args[0]
			}
			return runInit(cmd, dir)
		},
	}
	return cmd
}

func runInit(cmd *cobra.Command, dir string) error {
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("resolve working directory: %w", err)
		}
	}
	path := filepath.Join(dir, "agent.yaml")
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists", path)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(initAgentManifestYAML), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "created %s\n", path)
	return nil
}

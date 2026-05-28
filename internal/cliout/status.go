package cliout

import (
	"fmt"
	"io"
	"strings"
)

// StatusPanel is the data shown by `phrony status`.
type StatusPanel struct {
	RuntimeAddr    string
	CLIVersion     string
	RuntimeVersion string
	SchemaVersion  string
	Health         string
}

type statusRow struct {
	group string
	field string
	value string
}

// WriteStatus renders the logo and a borderless status table for the terminal.
func WriteStatus(w io.Writer, panel StatusPanel) error {
	schema := panel.SchemaVersion
	if schema == "" {
		schema = "—"
	}

	rows := []statusRow{
		{group: "CLI", field: "Version", value: panel.CLIVersion},
		{group: "Runtime", field: "Address", value: panel.RuntimeAddr},
		{group: "Runtime", field: "Version", value: panel.RuntimeVersion},
		{group: "Runtime", field: "Schema meta", value: schema},
		{group: "Runtime", field: "Health", value: formatHealth(panel.Health)},
	}

	if err := WriteLogo(w); err != nil {
		return err
	}
	var b strings.Builder
	writeTable(&b, rows)

	_, err := io.WriteString(w, b.String())
	return err
}

func writeTable(b *strings.Builder, rows []statusRow) {
	groupWidth := 0
	fieldWidth := 0
	for _, row := range rows {
		if n := len(row.group); n > groupWidth {
			groupWidth = n
		}
		if n := len(row.field); n > fieldWidth {
			fieldWidth = n
		}
	}
	for _, row := range rows {
		b.WriteString(fmt.Sprintf("%-*s  %-*s  %s\n", groupWidth, row.group, fieldWidth, row.field, row.value))
	}
}

func formatHealth(status string) string {
	switch status {
	case "SERVING":
		return "● SERVING"
	case "NOT_SERVING":
		return "○ NOT SERVING"
	case "SERVICE_UNKNOWN":
		return "? UNKNOWN"
	default:
		if status == "" {
			return "—"
		}
		return status
	}
}

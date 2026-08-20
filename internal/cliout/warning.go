package cliout

import (
	"io"
	"strings"

	"github.com/phrony-platform/runtime/internal/version"
)

const (
	ansiBoldYellow = "\x1b[1;33m"
	ansiYellow     = "\x1b[33m"
	ansiReset      = "\x1b[0m"
)

// WriteVersionMismatchWarning writes a version skew warning to w.
func WriteVersionMismatchWarning(w io.Writer, cliVersion, runtimeVersion string) error {
	_, err := io.WriteString(w, formatVersionMismatchWarning(UseColor(w), cliVersion, runtimeVersion))
	return err
}

func formatVersionMismatchWarning(color bool, cliVersion, runtimeVersion string) string {
	lines := version.VersionMismatchLines(cliVersion, runtimeVersion)
	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		if !color {
			b.WriteString(line)
			continue
		}
		if i == 0 {
			const prefix = "warning:"
			if strings.HasPrefix(line, prefix) {
				b.WriteString(ansiBoldYellow)
				b.WriteString(prefix)
				b.WriteString(ansiReset)
				b.WriteString(ansiYellow)
				b.WriteString(line[len(prefix):])
				b.WriteString(ansiReset)
				continue
			}
		}
		b.WriteString(ansiYellow)
		b.WriteString(line)
		b.WriteString(ansiReset)
	}
	b.WriteByte('\n')
	return b.String()
}

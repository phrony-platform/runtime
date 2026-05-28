package cliout

import (
	"io"
	"strings"
)

// phronyASCIILogo is the Phrony icon (wordmark text omitted).
const phronyASCIILogo = `    ÆÆÆÆÆ       ÆÆÆÆÆ
  ÆÆÆÆÆÆÆÆÆÆÆÆÆÆÆÆÆÆÆÆÆ
ÆÆÆÆÆÆÆÆÆÆÆÆÆÆÆÆÆÆÆÆÆÆÆÆÆ
  ÆÆÆÆÆÆÆÆÆ   ÆÆÆÆÆÆÆÆÆ
    ÆÆÆÆÆ       ÆÆÆÆÆ
     ÆÆÆ         ÆÆÆ
    ÆÆÆÆÆ       ÆÆÆÆÆ
  ÆÆÆÆÆÆÆÆÆ   ÆÆÆÆÆÆÆÆÆ
ÆÆÆÆÆÆÆÆÆÆÆÆÆÆÆÆÆÆÆÆÆÆÆÆÆ
  ÆÆÆÆÆÆÆÆÆÆÆÆÆÆÆÆÆÆÆÆÆ
    ÆÆÆÆÆ      ÆÆÆÆÆÆ`

const logoPaddingLines = 1

// WriteLogo prints the Phrony ASCII banner to w with blank lines above and below.
func WriteLogo(w io.Writer) error {
	if err := writeBlankLines(w, logoPaddingLines); err != nil {
		return err
	}
	for _, line := range strings.Split(phronyASCIILogo, "\n") {
		if _, err := io.WriteString(w, line); err != nil {
			return err
		}
		if _, err := io.WriteString(w, "\n"); err != nil {
			return err
		}
	}
	return writeBlankLines(w, logoPaddingLines)
}

func writeBlankLines(w io.Writer, n int) error {
	for range n {
		if _, err := io.WriteString(w, "\n"); err != nil {
			return err
		}
	}
	return nil
}

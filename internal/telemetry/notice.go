package telemetry

import (
	"fmt"
	"io"
)

const docsURL = "https://phrony.com/docs/runtime/telemetry"

// WriteNotice prints a one-line telemetry status notice when the daemon starts.
func WriteNotice(w io.Writer) error {
	if envDisablesTelemetry() {
		_, err := fmt.Fprintf(w,
			"Telemetry is disabled (DO_NOT_TRACK or DISABLE_TELEMETRY is set). %s\n",
			docsURL,
		)
		return err
	}

	cfg, err := LoadFileConfig()
	if err != nil {
		_, err := fmt.Fprintf(w, "Telemetry status unavailable (%v). %s\n", err, docsURL)
		return err
	}

	if cfg.Enabled {
		_, err := fmt.Fprintf(w,
			"Telemetry is enabled: coarse event counts (app version, platform, whitelisted event names). Disable with DO_NOT_TRACK=1 or phrony telemetry disable. %s\n",
			docsURL,
		)
		return err
	}

	_, err = fmt.Fprintf(w,
		"Telemetry is disabled. Enable with phrony telemetry enable. %s\n",
		docsURL,
	)
	return err
}

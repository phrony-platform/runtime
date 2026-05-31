package core

import (
	"os"
	"os/user"
	"strings"
)

func resolveActor(requestActor string) string {
	if a := strings.TrimSpace(requestActor); a != "" {
		return a
	}
	if a := strings.TrimSpace(os.Getenv("PHRONY_ACTOR")); a != "" {
		return a
	}
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return "unknown"
}

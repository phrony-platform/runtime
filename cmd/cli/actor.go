package main

import (
	"os"
	"os/user"
	"strings"
)

func cliActor() string {
	if a := strings.TrimSpace(os.Getenv("PHRONY_ACTOR")); a != "" {
		return a
	}
	if u, err := user.Current(); err == nil {
		if name := strings.TrimSpace(u.Username); name != "" {
			return name
		}
	}
	return ""
}

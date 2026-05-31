package core

import "github.com/google/uuid"

func newRunSessionID() string {
	return "run_" + uuid.NewString()
}

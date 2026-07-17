package e2e_test

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	integrationSetup()
	code := m.Run()
	integrationTeardown()
	os.Exit(code)
}

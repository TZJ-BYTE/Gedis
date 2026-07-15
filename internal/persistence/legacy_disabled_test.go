package persistence

import (
	"os"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	_, err := OpenLSMEnergy(os.TempDir(), DefaultOptions())
	if err != nil && strings.Contains(err.Error(), "legacy LSM engine is disabled") {
		os.Exit(0)
	}
	os.Exit(m.Run())
}

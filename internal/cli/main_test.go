package cli

import (
	"os"
	"testing"
)

// TestMain keeps CLI tests on the workspace data layout so assertions that
// look under <workspace>/.mincode stay valid. Production default is global.
func TestMain(m *testing.M) {
	_ = os.Setenv("MINCODE_DATA_LOCATION", "workspace")
	os.Exit(m.Run())
}

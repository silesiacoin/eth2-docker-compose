package testing

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/prysmaticlabs/prysm/shared/testutil/require"
	"github.com/prysmaticlabs/prysm/validator/db/kv"
)

func TestClearDB(t *testing.T) {
	// Setting up manually is required, since SetupDB() will also register a teardown procedure.
	testDB, err := kv.NewKVStore(t.TempDir(), [][48]byte{})
	require.NoError(t, err, "Failed to instantiate DB")
	require.NoError(t, testDB.ClearDB())

	if _, err := os.Stat(filepath.Join(testDB.DatabasePath(), "validator.db")); !os.IsNotExist(err) {
		t.Fatalf("DB was not cleared: %v", err)
	}
}

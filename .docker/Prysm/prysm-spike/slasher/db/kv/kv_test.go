package kv

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/prysmaticlabs/prysm/shared/testutil/require"
	"github.com/sirupsen/logrus"
)

func TestMain(m *testing.M) {
	run := func() int {
		logrus.SetLevel(logrus.DebugLevel)
		logrus.SetOutput(ioutil.Discard)

		return m.Run()
	}
	os.Exit(run())
}

func setupDB(t testing.TB) *Store {
	cfg := &Config{}
	db, err := NewKVStore(t.TempDir(), cfg)
	require.NoError(t, err, "Failed to instantiate DB")
	t.Cleanup(func() {
		require.NoError(t, db.Close(), "Failed to close database")
	})
	return db
}

func setupDBDiffCacheSize(t testing.TB, cacheSize int) *Store {
	cfg := &Config{SpanCacheSize: cacheSize}
	db, err := NewKVStore(t.TempDir(), cfg)
	require.NoError(t, err, "Failed to instantiate DB")
	t.Cleanup(func() {
		require.NoError(t, db.Close(), "Failed to close database")
	})
	return db
}

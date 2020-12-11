package validator

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/prysmaticlabs/prysm/shared/params"
	"github.com/sirupsen/logrus"
)

func TestMain(m *testing.M) {
	run := func() int {
		logrus.SetLevel(logrus.DebugLevel)
		logrus.SetOutput(ioutil.Discard)
		// Use minimal config to reduce test setup time.
		prevConfig := params.BeaconConfig().Copy()
		defer params.OverrideBeaconConfig(prevConfig)
		params.OverrideBeaconConfig(params.MinimalSpecConfig())

		return m.Run()
	}
	os.Exit(run())
}

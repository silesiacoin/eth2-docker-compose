package sync

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/prysmaticlabs/prysm/beacon-chain/flags"
	"github.com/sirupsen/logrus"
)

func TestMain(m *testing.M) {
	run := func() int {
		logrus.SetLevel(logrus.DebugLevel)
		logrus.SetOutput(ioutil.Discard)

		resetFlags := flags.Get()
		flags.Init(&flags.GlobalFlags{
			BlockBatchLimit:            64,
			BlockBatchLimitBurstFactor: 10,
		})
		defer func() {
			flags.Init(resetFlags)
		}()
		return m.Run()
	}
	os.Exit(run())
}

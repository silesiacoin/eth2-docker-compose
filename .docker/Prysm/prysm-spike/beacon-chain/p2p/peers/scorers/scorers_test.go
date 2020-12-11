package scorers_test

import (
	"io/ioutil"
	"math"
	"os"
	"testing"

	"github.com/prysmaticlabs/prysm/beacon-chain/flags"
	"github.com/prysmaticlabs/prysm/beacon-chain/p2p/peers/scorers"
	"github.com/prysmaticlabs/prysm/shared/featureconfig"
	"github.com/sirupsen/logrus"
)

func TestMain(m *testing.M) {
	run := func() int {
		logrus.SetLevel(logrus.DebugLevel)
		logrus.SetOutput(ioutil.Discard)

		resetCfg := featureconfig.InitWithReset(&featureconfig.Flags{
			EnablePeerScorer: true,
		})
		defer resetCfg()

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

// roundScore returns score rounded in accordance with the score manager's rounding factor.
func roundScore(score float64) float64 {
	return math.Round(score*scorers.ScoreRoundingFactor) / scorers.ScoreRoundingFactor
}
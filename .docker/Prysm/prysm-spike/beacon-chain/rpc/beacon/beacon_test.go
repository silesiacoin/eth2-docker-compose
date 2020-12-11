package beacon

import (
	"os"
	"testing"

	"github.com/prysmaticlabs/prysm/beacon-chain/flags"
	"github.com/prysmaticlabs/prysm/shared/params"
)

func TestMain(m *testing.M) {
	run := func() int {
		// Use minimal config to reduce test setup time.
		prevConfig := params.BeaconConfig().Copy()
		defer params.OverrideBeaconConfig(prevConfig)
		params.OverrideBeaconConfig(params.MinimalSpecConfig())

		resetFlags := flags.Get()
		flags.Init(&flags.GlobalFlags{
			MinimumSyncPeers: 30,
		})
		defer func() {
			flags.Init(resetFlags)
		}()

		return m.Run()
	}
	os.Exit(run())
}

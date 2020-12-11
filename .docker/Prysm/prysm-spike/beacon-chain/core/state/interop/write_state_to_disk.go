package interop

import (
	"fmt"
	"os"
	"path"

	stateTrie "github.com/prysmaticlabs/prysm/beacon-chain/state"
	"github.com/prysmaticlabs/prysm/shared/featureconfig"
	"github.com/prysmaticlabs/prysm/shared/fileutil"
)

// WriteStateToDisk as a state ssz. Writes to temp directory. Debug!
func WriteStateToDisk(state *stateTrie.BeaconState) {
	if !featureconfig.Get().WriteSSZStateTransitions {
		return
	}
	fp := path.Join(os.TempDir(), fmt.Sprintf("beacon_state_%d.ssz", state.Slot()))
	log.Warnf("Writing state to disk at %s", fp)
	enc, err := state.InnerStateUnsafe().MarshalSSZ()
	if err != nil {
		log.WithError(err).Error("Failed to ssz encode state")
		return
	}
	if err := fileutil.WriteFile(fp, enc); err != nil {
		log.WithError(err).Error("Failed to write to disk")
	}
}

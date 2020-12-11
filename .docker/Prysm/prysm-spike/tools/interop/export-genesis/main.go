package main

import (
	"context"
	"fmt"
	"os"

	"github.com/prysmaticlabs/prysm/beacon-chain/cache"
	"github.com/prysmaticlabs/prysm/beacon-chain/db"
	"github.com/prysmaticlabs/prysm/shared/fileutil"
)

// A basic tool to extract genesis.ssz from existing beaconchain.db.
// ex:
//   bazel run //tools/interop/export-genesis:export-genesis -- /tmp/data/beaconchaindata /tmp/genesis.ssz
func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: ./main /path/to/datadir /path/to/output/genesis.ssz")
		os.Exit(1)
	}

	fmt.Printf("Reading db at %s and writing ssz output to %s.\n", os.Args[1], os.Args[2])

	d, err := db.NewDB(os.Args[1], cache.NewStateSummaryCache())
	if err != nil {
		panic(err)
	}
	defer func() {
		if err := d.Close(); err != nil {
			panic(err)
		}
	}()
	gs, err := d.GenesisState(context.Background())
	if err != nil {
		panic(err)
	}
	if gs == nil {
		panic("nil genesis state")
	}
	b, err := gs.InnerStateUnsafe().MarshalSSZ()
	if err != nil {
		panic(err)
	}
	if err := fileutil.WriteFile(os.Args[2], b); err != nil {
		panic(err)
	}
	fmt.Println("done")
}

// Package nodev1 defines a gRPC node service implementation, providing
// useful endpoints for checking a node's sync status, peer info,
// genesis data, and version information.
package nodev1

import (
	"github.com/prysmaticlabs/prysm/beacon-chain/blockchain"
	"github.com/prysmaticlabs/prysm/beacon-chain/db"
	"github.com/prysmaticlabs/prysm/beacon-chain/p2p"
	"github.com/prysmaticlabs/prysm/beacon-chain/sync"
	"google.golang.org/grpc"
)

// Server defines a server implementation of the gRPC Node service,
// providing RPC endpoints for verifying a beacon node's sync status, genesis and
// version information.
type Server struct {
	SyncChecker        sync.Checker
	Server             *grpc.Server
	BeaconDB           db.ReadOnlyDatabase
	PeersFetcher       p2p.PeersProvider
	PeerManager        p2p.PeerManager
	GenesisTimeFetcher blockchain.TimeFetcher
	GenesisFetcher     blockchain.GenesisFetcher
}

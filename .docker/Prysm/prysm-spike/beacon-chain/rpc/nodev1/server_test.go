package nodev1

import (
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1"
)

var _ ethpb.BeaconNodeServer = (*Server)(nil)

// Package testing includes useful helpers for slasher-related
// unit tests.
package testing

import (
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/prysm/shared/rand"
)

// SignedBlockHeader given slot, proposer index this function generates signed block header.
// with random bytes as its signature.
func SignedBlockHeader(slot, proposerIdx uint64) (*ethpb.SignedBeaconBlockHeader, error) {
	sig, err := genRandomByteArray(96)
	if err != nil {
		return nil, err
	}
	root := [32]byte{1, 2, 3}
	return &ethpb.SignedBeaconBlockHeader{
		Header: &ethpb.BeaconBlockHeader{
			ProposerIndex: proposerIdx,
			Slot:          slot,
			ParentRoot:    root[:],
			StateRoot:     root[:],
			BodyRoot:      root[:],
		},
		Signature: sig,
	}, nil
}

// BlockHeader given slot, proposer index this function generates block header.
func BlockHeader(slot, proposerIdx uint64) (*ethpb.BeaconBlockHeader, error) {
	root := [32]byte{1, 2, 3}
	return &ethpb.BeaconBlockHeader{
		ProposerIndex: proposerIdx,
		Slot:          slot,
		ParentRoot:    root[:],
		StateRoot:     root[:],
		BodyRoot:      root[:],
	}, nil
}

func genRandomByteArray(length int) ([]byte, error) {
	blk := make([]byte, length)
	randGen := rand.NewDeterministicGenerator()
	_, err := randGen.Read(blk)
	return blk, err
}

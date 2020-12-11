package blocks

import (
	"bytes"
	"context"
	"errors"

	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	stateTrie "github.com/prysmaticlabs/prysm/beacon-chain/state"
	"github.com/prysmaticlabs/prysm/shared/params"
)

// ProcessEth1DataInBlock is an operation performed on each
// beacon block to ensure the ETH1 data votes are processed
// into the beacon state.
//
// Official spec definition:
//   def process_eth1_data(state: BeaconState, body: BeaconBlockBody) -> None:
//    state.eth1_data_votes.append(body.eth1_data)
//    if state.eth1_data_votes.count(body.eth1_data) * 2 > EPOCHS_PER_ETH1_VOTING_PERIOD * SLOTS_PER_EPOCH:
//        state.latest_eth1_data = body.eth1_data
func ProcessEth1DataInBlock(_ context.Context, beaconState *stateTrie.BeaconState, b *ethpb.SignedBeaconBlock) (*stateTrie.BeaconState, error) {
	block := b.Block
	if beaconState == nil {
		return nil, errors.New("nil state")
	}
	if block == nil || block.Body == nil {
		return nil, errors.New("nil block or block withought body")
	}
	if err := beaconState.AppendEth1DataVotes(block.Body.Eth1Data); err != nil {
		return nil, err
	}
	hasSupport, err := Eth1DataHasEnoughSupport(beaconState, block.Body.Eth1Data)
	if err != nil {
		return nil, err
	}
	if hasSupport {
		if err := beaconState.SetEth1Data(block.Body.Eth1Data); err != nil {
			return nil, err
		}
	}
	return beaconState, nil
}

// AreEth1DataEqual checks equality between two eth1 data objects.
func AreEth1DataEqual(a, b *ethpb.Eth1Data) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.DepositCount == b.DepositCount &&
		bytes.Equal(a.BlockHash, b.BlockHash) &&
		bytes.Equal(a.DepositRoot, b.DepositRoot)
}

// Eth1DataHasEnoughSupport returns true when the given eth1data has more than 50% votes in the
// eth1 voting period. A vote is cast by including eth1data in a block and part of state processing
// appends eth1data to the state in the Eth1DataVotes list. Iterating through this list checks the
// votes to see if they match the eth1data.
func Eth1DataHasEnoughSupport(beaconState *stateTrie.BeaconState, data *ethpb.Eth1Data) (bool, error) {
	voteCount := uint64(0)
	data = stateTrie.CopyETH1Data(data)

	for _, vote := range beaconState.Eth1DataVotes() {
		if AreEth1DataEqual(vote, data) {
			voteCount++
		}
	}

	// If 50+% majority converged on the same eth1data, then it has enough support to update the
	// state.
	support := params.BeaconConfig().EpochsPerEth1VotingPeriod * params.BeaconConfig().SlotsPerEpoch
	return voteCount*2 > support, nil
}

// Package precompute provides gathering of nicely-structured
// data important to feed into epoch processing, such as attesting
// records and balances, for faster computation.
package precompute

import (
	"context"

	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	stateTrie "github.com/prysmaticlabs/prysm/beacon-chain/state"
	"github.com/prysmaticlabs/prysm/shared/params"
	"go.opencensus.io/trace"
)

// New gets called at the beginning of process epoch cycle to return
// pre computed instances of validators attesting records and total
// balances attested in an epoch.
func New(ctx context.Context, state *stateTrie.BeaconState) ([]*Validator, *Balance, error) {
	ctx, span := trace.StartSpan(ctx, "precomputeEpoch.New")
	defer span.End()
	pValidators := make([]*Validator, state.NumValidators())
	pBal := &Balance{}

	currentEpoch := helpers.CurrentEpoch(state)
	prevEpoch := helpers.PrevEpoch(state)

	if err := state.ReadFromEveryValidator(func(idx int, val stateTrie.ReadOnlyValidator) error {
		// Was validator withdrawable or slashed
		withdrawable := prevEpoch+1 >= val.WithdrawableEpoch()
		pVal := &Validator{
			IsSlashed:                    val.Slashed(),
			IsWithdrawableCurrentEpoch:   withdrawable,
			CurrentEpochEffectiveBalance: val.EffectiveBalance(),
		}
		// Was validator active current epoch
		if helpers.IsActiveValidatorUsingTrie(val, currentEpoch) {
			pVal.IsActiveCurrentEpoch = true
			pBal.ActiveCurrentEpoch += val.EffectiveBalance()
		}
		// Was validator active previous epoch
		if helpers.IsActiveValidatorUsingTrie(val, prevEpoch) {
			pVal.IsActivePrevEpoch = true
			pBal.ActivePrevEpoch += val.EffectiveBalance()
		}
		// Set inclusion slot and inclusion distance to be max, they will be compared and replaced
		// with the lower values
		pVal.InclusionSlot = params.BeaconConfig().FarFutureEpoch
		pVal.InclusionDistance = params.BeaconConfig().FarFutureEpoch

		pValidators[idx] = pVal
		return nil
	}); err != nil {
		return nil, nil, errors.Wrap(err, "failed to initialize precompute")
	}
	return pValidators, pBal, nil
}
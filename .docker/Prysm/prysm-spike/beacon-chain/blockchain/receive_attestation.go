package blockchain

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/feed"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	"github.com/prysmaticlabs/prysm/beacon-chain/state"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
	"github.com/prysmaticlabs/prysm/shared/params"
	"github.com/prysmaticlabs/prysm/shared/slotutil"
	"github.com/sirupsen/logrus"
	"go.opencensus.io/trace"
)

// AttestationReceiver interface defines the methods of chain service receive and processing new attestations.
type AttestationReceiver interface {
	ReceiveAttestationNoPubsub(ctx context.Context, att *ethpb.Attestation) error
	AttestationPreState(ctx context.Context, att *ethpb.Attestation) (*state.BeaconState, error)
	VerifyLmdFfgConsistency(ctx context.Context, att *ethpb.Attestation) error
	VerifyFinalizedConsistency(ctx context.Context, root []byte) error
}

// ReceiveAttestationNoPubsub is a function that defines the operations that are performed on
// attestation that is received from regular sync. The operations consist of:
//  1. Validate attestation, update validator's latest vote
//  2. Apply fork choice to the processed attestation
//  3. Save latest head info
func (s *Service) ReceiveAttestationNoPubsub(ctx context.Context, att *ethpb.Attestation) error {
	ctx, span := trace.StartSpan(ctx, "beacon-chain.blockchain.ReceiveAttestationNoPubsub")
	defer span.End()

	_, err := s.onAttestation(ctx, att)
	if err != nil {
		return errors.Wrap(err, "could not process attestation")
	}

	if err := s.updateHead(ctx, s.getJustifiedBalances()); err != nil {
		log.Warnf("Resolving fork due to new attestation: %v", err)
		return nil
	}

	return nil
}

// AttestationPreState returns the pre state of attestation.
func (s *Service) AttestationPreState(ctx context.Context, att *ethpb.Attestation) (*state.BeaconState, error) {
	ss, err := helpers.StartSlot(att.Data.Target.Epoch)
	if err != nil {
		return nil, err
	}
	if err := helpers.ValidateSlotClock(ss, uint64(s.genesisTime.Unix())); err != nil {
		return nil, err
	}
	return s.getAttPreState(ctx, att.Data.Target)
}

// VerifyLmdFfgConsistency verifies that attestation's LMD and FFG votes are consistency to each other.
func (s *Service) VerifyLmdFfgConsistency(ctx context.Context, a *ethpb.Attestation) error {
	return s.verifyLMDFFGConsistent(ctx, a.Data.Target.Epoch, a.Data.Target.Root, a.Data.BeaconBlockRoot)
}

// VerifyFinalizedConsistency verifies input root is consistent with finalized store.
// When the input root is not be consistent with finalized store then we know it is not
// on the finalized check point that leads to current canonical chain and should be rejected accordingly.
func (s *Service) VerifyFinalizedConsistency(ctx context.Context, root []byte) error {
	// A canonical root implies the root to has an ancestor that aligns with finalized check point.
	// In this case, we could exit early to save on additional computation.
	if s.forkChoiceStore.IsCanonical(bytesutil.ToBytes32(root)) {
		return nil
	}

	f := s.FinalizedCheckpt()
	ss, err := helpers.StartSlot(f.Epoch)
	if err != nil {
		return err
	}
	r, err := s.ancestor(ctx, root, ss)
	if err != nil {
		return err
	}
	if !bytes.Equal(f.Root, r) {
		return errors.New("Root and finalized store are not consistent")
	}

	return nil
}

// This processes attestations from the attestation pool to account for validator votes and fork choice.
func (s *Service) processAttestation(subscribedToStateEvents chan struct{}) {
	// Wait for state to be initialized.
	stateChannel := make(chan *feed.Event, 1)
	stateSub := s.stateNotifier.StateFeed().Subscribe(stateChannel)
	subscribedToStateEvents <- struct{}{}
	<-stateChannel
	stateSub.Unsubscribe()

	if s.genesisTime.IsZero() {
		log.Warn("ProcessAttestations routine waiting for genesis time")
		for s.genesisTime.IsZero() {
			time.Sleep(1 * time.Second)
		}
		log.Warn("Genesis time received, now available to process attestations")
	}

	st := slotutil.GetSlotTicker(s.genesisTime, params.BeaconConfig().SecondsPerSlot)
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-st.C():
			ctx := s.ctx
			atts := s.attPool.ForkchoiceAttestations()
			for _, a := range atts {
				// Based on the spec, don't process the attestation until the subsequent slot.
				// This delays consideration in the fork choice until their slot is in the past.
				// https://github.com/ethereum/eth2.0-specs/blob/dev/specs/phase0/fork-choice.md#validate_on_attestation
				nextSlot := a.Data.Slot + 1
				if err := helpers.VerifySlotTime(uint64(s.genesisTime.Unix()), nextSlot, params.BeaconNetworkConfig().MaximumGossipClockDisparity); err != nil {
					continue
				}

				hasState := s.stateGen.StateSummaryExists(ctx, bytesutil.ToBytes32(a.Data.BeaconBlockRoot))
				hasBlock := s.hasBlock(ctx, bytesutil.ToBytes32(a.Data.BeaconBlockRoot))
				if !(hasState && hasBlock) {
					continue
				}

				if err := s.attPool.DeleteForkchoiceAttestation(a); err != nil {
					log.WithError(err).Error("Could not delete fork choice attestation in pool")
				}

				if !helpers.VerifyCheckpointEpoch(a.Data.Target, s.genesisTime) {
					continue
				}

				if err := s.ReceiveAttestationNoPubsub(ctx, a); err != nil {
					log.WithFields(logrus.Fields{
						"slot":             a.Data.Slot,
						"committeeIndex":   a.Data.CommitteeIndex,
						"beaconBlockRoot":  fmt.Sprintf("%#x", bytesutil.Trunc(a.Data.BeaconBlockRoot)),
						"targetRoot":       fmt.Sprintf("%#x", bytesutil.Trunc(a.Data.Target.Root)),
						"aggregationCount": a.AggregationBits.Count(),
					}).WithError(err).Warn("Could not receive attestation in chain service")
				}
			}
		}
	}
}

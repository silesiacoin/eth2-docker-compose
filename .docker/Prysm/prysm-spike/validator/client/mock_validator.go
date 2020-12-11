package client

import (
	"context"
	"time"

	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/prysm/shared/timeutils"
)

var _ Validator = (*FakeValidator)(nil)

// FakeValidator for mocking.
type FakeValidator struct {
	DoneCalled                        bool
	WaitForWalletInitializationCalled bool
	WaitForActivationCalled           bool
	WaitForChainStartCalled           bool
	WaitForSyncCalled                 bool
	SlasherReadyCalled                bool
	NextSlotCalled                    bool
	CanonicalHeadSlotCalled           bool
	UpdateDutiesCalled                bool
	UpdateProtectionsCalled           bool
	RoleAtCalled                      bool
	AttestToBlockHeadCalled           bool
	ProposeBlockCalled                bool
	LogValidatorGainsAndLossesCalled  bool
	SaveProtectionsCalled             bool
	DeleteProtectionCalled            bool
	SlotDeadlineCalled                bool
	ProposeBlockArg1                  uint64
	AttestToBlockHeadArg1             uint64
	RoleAtArg1                        uint64
	UpdateDutiesArg1                  uint64
	NextSlotRet                       <-chan uint64
	PublicKey                         string
	UpdateDutiesRet                   error
	RolesAtRet                        []ValidatorRole
	Balances                          map[[48]byte]uint64
	IndexToPubkeyMap                  map[uint64][48]byte
	PubkeyToIndexMap                  map[[48]byte]uint64
	PubkeysToStatusesMap              map[[48]byte]ethpb.ValidatorStatus
}

type ctxKey string

var allValidatorsAreExitedCtxKey = ctxKey("exited")

// Done for mocking.
func (fv *FakeValidator) Done() {
	fv.DoneCalled = true
}

// WaitForWalletInitialization for mocking.
func (fv *FakeValidator) WaitForWalletInitialization(_ context.Context) error {
	fv.WaitForWalletInitializationCalled = true
	return nil
}

// WaitForChainStart for mocking.
func (fv *FakeValidator) WaitForChainStart(_ context.Context) error {
	fv.WaitForChainStartCalled = true
	return nil
}

// WaitForActivation for mocking.
func (fv *FakeValidator) WaitForActivation(_ context.Context) error {
	fv.WaitForActivationCalled = true
	return nil
}

// WaitForSync for mocking.
func (fv *FakeValidator) WaitForSync(_ context.Context) error {
	fv.WaitForSyncCalled = true
	return nil
}

// SlasherReady for mocking.
func (fv *FakeValidator) SlasherReady(_ context.Context) error {
	fv.SlasherReadyCalled = true
	return nil
}

// CanonicalHeadSlot for mocking.
func (fv *FakeValidator) CanonicalHeadSlot(_ context.Context) (uint64, error) {
	fv.CanonicalHeadSlotCalled = true
	return 0, nil
}

// SlotDeadline for mocking.
func (fv *FakeValidator) SlotDeadline(_ uint64) time.Time {
	fv.SlotDeadlineCalled = true
	return timeutils.Now()
}

// NextSlot for mocking.
func (fv *FakeValidator) NextSlot() <-chan uint64 {
	fv.NextSlotCalled = true
	return fv.NextSlotRet
}

// UpdateDuties for mocking.
func (fv *FakeValidator) UpdateDuties(_ context.Context, slot uint64) error {
	fv.UpdateDutiesCalled = true
	fv.UpdateDutiesArg1 = slot
	return fv.UpdateDutiesRet
}

// UpdateProtections for mocking.
func (fv *FakeValidator) UpdateProtections(_ context.Context, _ uint64) error {
	fv.UpdateProtectionsCalled = true
	return nil
}

// LogValidatorGainsAndLosses for mocking.
func (fv *FakeValidator) LogValidatorGainsAndLosses(_ context.Context, _ uint64) error {
	fv.LogValidatorGainsAndLossesCalled = true
	return nil
}

// ResetAttesterProtectionData for mocking.
func (fv *FakeValidator) ResetAttesterProtectionData() {
	fv.DeleteProtectionCalled = true
}

// RolesAt for mocking.
func (fv *FakeValidator) RolesAt(_ context.Context, slot uint64) (map[[48]byte][]ValidatorRole, error) {
	fv.RoleAtCalled = true
	fv.RoleAtArg1 = slot
	vr := make(map[[48]byte][]ValidatorRole)
	vr[[48]byte{1}] = fv.RolesAtRet
	return vr, nil
}

// SubmitAttestation for mocking.
func (fv *FakeValidator) SubmitAttestation(_ context.Context, slot uint64, _ [48]byte) {
	fv.AttestToBlockHeadCalled = true
	fv.AttestToBlockHeadArg1 = slot
}

// ProposeBlock for mocking.
func (fv *FakeValidator) ProposeBlock(_ context.Context, slot uint64, _ [48]byte) {
	fv.ProposeBlockCalled = true
	fv.ProposeBlockArg1 = slot
}

// SubmitAggregateAndProof for mocking.
func (fv *FakeValidator) SubmitAggregateAndProof(_ context.Context, _ uint64, _ [48]byte) {}

// LogAttestationsSubmitted for mocking.
func (fv *FakeValidator) LogAttestationsSubmitted() {}

// UpdateDomainDataCaches for mocking.
func (fv *FakeValidator) UpdateDomainDataCaches(context.Context, uint64) {}

// BalancesByPubkeys for mocking.
func (fv *FakeValidator) BalancesByPubkeys(_ context.Context) map[[48]byte]uint64 {
	return fv.Balances
}

// IndicesToPubkeys for mocking.
func (fv *FakeValidator) IndicesToPubkeys(_ context.Context) map[uint64][48]byte {
	return fv.IndexToPubkeyMap
}

// PubkeysToIndices for mocking.
func (fv *FakeValidator) PubkeysToIndices(_ context.Context) map[[48]byte]uint64 {
	return fv.PubkeyToIndexMap
}

// PubkeysToStatuses for mocking.
func (fv *FakeValidator) PubkeysToStatuses(_ context.Context) map[[48]byte]ethpb.ValidatorStatus {
	return fv.PubkeysToStatusesMap
}

// AllValidatorsAreExited for mocking
func (fv *FakeValidator) AllValidatorsAreExited(ctx context.Context) (bool, error) {
	if ctx.Value(allValidatorsAreExitedCtxKey) == nil {
		return false, nil
	}
	return ctx.Value(allValidatorsAreExitedCtxKey).(bool), nil
}

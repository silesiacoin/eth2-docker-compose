package kv

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
	"github.com/prysmaticlabs/prysm/shared/params"
	bolt "go.etcd.io/bbolt"
	"go.opencensus.io/trace"
)

// ProposalHistoryForPubkey for a validator public key.
type ProposalHistoryForPubkey struct {
	Proposals []Proposal
}

// Proposal representation for a validator public key.
type Proposal struct {
	Slot        uint64 `json:"slot"`
	SigningRoot []byte `json:"signing_root"`
}

// ProposedPublicKeys retrieves all public keys in our proposals history bucket.
func (store *Store) ProposedPublicKeys(ctx context.Context) ([][48]byte, error) {
	ctx, span := trace.StartSpan(ctx, "Validator.ProposedPublicKeys")
	defer span.End()
	var err error
	proposedPublicKeys := make([][48]byte, 0)
	err = store.view(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(newHistoricProposalsBucket)
		return bucket.ForEach(func(key []byte, _ []byte) error {
			pubKeyBytes := [48]byte{}
			copy(pubKeyBytes[:], key)
			proposedPublicKeys = append(proposedPublicKeys, pubKeyBytes)
			return nil
		})
	})
	return proposedPublicKeys, err
}

// ProposalHistoryForSlot accepts a validator public key and returns the corresponding signing root as well
// as a boolean that tells us if we have a proposal history stored at the slot. It is possible we have proposed
// a slot but stored a nil signing root, so the boolean helps give full information.
func (store *Store) ProposalHistoryForSlot(ctx context.Context, publicKey [48]byte, slot uint64) ([32]byte, bool, error) {
	ctx, span := trace.StartSpan(ctx, "Validator.ProposalHistoryForSlot")
	defer span.End()

	var err error
	var proposalExists bool
	signingRoot := [32]byte{}
	err = store.update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(newHistoricProposalsBucket)
		valBucket, err := bucket.CreateBucketIfNotExists(publicKey[:])
		if err != nil {
			return fmt.Errorf("could not create bucket for public key %#x", publicKey[:])
		}
		signingRootBytes := valBucket.Get(bytesutil.Uint64ToBytesBigEndian(slot))
		if signingRootBytes == nil {
			return nil
		}
		proposalExists = true
		copy(signingRoot[:], signingRootBytes)
		return nil
	})
	return signingRoot, proposalExists, err
}

// SaveProposalHistoryForSlot saves the proposal history for the requested validator public key.
// We also check if the incoming proposal slot is lower than the lowest signed proposal slot
// for the validator and override its value on disk.
func (store *Store) SaveProposalHistoryForSlot(ctx context.Context, pubKey [48]byte, slot uint64, signingRoot []byte) error {
	ctx, span := trace.StartSpan(ctx, "Validator.SaveProposalHistoryForEpoch")
	defer span.End()

	err := store.update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(newHistoricProposalsBucket)
		valBucket, err := bucket.CreateBucketIfNotExists(pubKey[:])
		if err != nil {
			return fmt.Errorf("could not create bucket for public key %#x", pubKey)
		}

		// If the incoming slot is lower than the lowest signed proposal slot, override.
		lowestSignedBkt := tx.Bucket(lowestSignedProposalsBucket)
		lowestSignedProposalBytes := lowestSignedBkt.Get(pubKey[:])
		var lowestSignedProposalSlot uint64
		if len(lowestSignedProposalBytes) >= 8 {
			lowestSignedProposalSlot = bytesutil.BytesToUint64BigEndian(lowestSignedProposalBytes)
		}
		if len(lowestSignedProposalBytes) == 0 || slot < lowestSignedProposalSlot {
			if err := lowestSignedBkt.Put(pubKey[:], bytesutil.Uint64ToBytesBigEndian(slot)); err != nil {
				return err
			}
		}

		// If the incoming slot is higher than the highest signed proposal slot, override.
		highestSignedBkt := tx.Bucket(highestSignedProposalsBucket)
		highestSignedProposalBytes := highestSignedBkt.Get(pubKey[:])
		var highestSignedProposalSlot uint64
		if len(highestSignedProposalBytes) >= 8 {
			highestSignedProposalSlot = bytesutil.BytesToUint64BigEndian(highestSignedProposalBytes)
		}
		if len(highestSignedProposalBytes) == 0 || slot > highestSignedProposalSlot {
			if err := highestSignedBkt.Put(pubKey[:], bytesutil.Uint64ToBytesBigEndian(slot)); err != nil {
				return err
			}
		}

		if err := valBucket.Put(bytesutil.Uint64ToBytesBigEndian(slot), signingRoot); err != nil {
			return err
		}
		return pruneProposalHistoryBySlot(valBucket, slot)
	})
	return err
}

// LowestSignedProposal returns the lowest signed proposal slot for a validator public key.
// If no data exists, returning 0 is a sensible default.
func (store *Store) LowestSignedProposal(ctx context.Context, publicKey [48]byte) (uint64, error) {
	ctx, span := trace.StartSpan(ctx, "Validator.LowestSignedProposal")
	defer span.End()

	var err error
	var lowestSignedProposalSlot uint64
	err = store.view(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(lowestSignedProposalsBucket)
		lowestSignedProposalBytes := bucket.Get(publicKey[:])
		// 8 because bytesutil.BytesToUint64BigEndian will return 0 if input is less than 8 bytes.
		if len(lowestSignedProposalBytes) < 8 {
			return nil
		}
		lowestSignedProposalSlot = bytesutil.BytesToUint64BigEndian(lowestSignedProposalBytes)
		return nil
	})
	return lowestSignedProposalSlot, err
}

// HighestSignedProposal returns the highest signed proposal slot for a validator public key.
// If no data exists, returning 0 is a sensible default.
func (store *Store) HighestSignedProposal(ctx context.Context, publicKey [48]byte) (uint64, error) {
	ctx, span := trace.StartSpan(ctx, "Validator.HighestSignedProposal")
	defer span.End()

	var err error
	var highestSignedProposalSlot uint64
	err = store.view(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(highestSignedProposalsBucket)
		highestSignedProposalBytes := bucket.Get(publicKey[:])
		// 8 because bytesutil.BytesToUint64BigEndian will return 0 if input is less than 8 bytes.
		if len(highestSignedProposalBytes) < 8 {
			return nil
		}
		highestSignedProposalSlot = bytesutil.BytesToUint64BigEndian(highestSignedProposalBytes)
		return nil
	})
	return highestSignedProposalSlot, err
}

func pruneProposalHistoryBySlot(valBucket *bolt.Bucket, newestSlot uint64) error {
	c := valBucket.Cursor()
	for k, _ := c.First(); k != nil; k, _ = c.First() {
		slot := bytesutil.BytesToUint64BigEndian(k)
		epoch := helpers.SlotToEpoch(slot)
		newestEpoch := helpers.SlotToEpoch(newestSlot)
		// Only delete epochs that are older than the weak subjectivity period.
		if epoch+params.BeaconConfig().WeakSubjectivityPeriod <= newestEpoch {
			if err := c.Delete(); err != nil {
				return errors.Wrapf(err, "could not prune epoch %d in proposal history", epoch)
			}
		} else {
			// If starting from the oldest, we dont find anything prunable, stop pruning.
			break
		}
	}
	return nil
}

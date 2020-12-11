package protoarray

import (
	"context"

	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/shared/params"
	"go.opencensus.io/trace"
)

// This defines the minimal number of block nodes that can be in the tree
// before getting pruned upon new finalization.
const defaultPruneThreshold = 256

// This tracks the last reported head root. Used for metrics.
var lastHeadRoot [32]byte

// New initializes a new fork choice store.
func New(justifiedEpoch, finalizedEpoch uint64, finalizedRoot [32]byte) *ForkChoice {
	s := &Store{
		justifiedEpoch: justifiedEpoch,
		finalizedEpoch: finalizedEpoch,
		finalizedRoot:  finalizedRoot,
		nodes:          make([]*Node, 0),
		nodesIndices:   make(map[[32]byte]uint64),
		canonicalNodes: make(map[[32]byte]bool),
		pruneThreshold: defaultPruneThreshold,
	}

	b := make([]uint64, 0)
	v := make([]Vote, 0)

	return &ForkChoice{store: s, balances: b, votes: v}
}

// Head returns the head root from fork choice store.
// It firsts computes validator's balance changes then recalculates block tree from leaves to root.
func (f *ForkChoice) Head(ctx context.Context, justifiedEpoch uint64, justifiedRoot [32]byte, justifiedStateBalances []uint64, finalizedEpoch uint64) ([32]byte, error) {
	ctx, span := trace.StartSpan(ctx, "protoArrayForkChoice.Head")
	defer span.End()
	f.votesLock.Lock()
	defer f.votesLock.Unlock()

	calledHeadCount.Inc()

	newBalances := justifiedStateBalances

	// Using the write lock here because `updateCanonicalNodes` that gets called subsequently requires a write operation.
	f.store.nodesLock.Lock()
	defer f.store.nodesLock.Unlock()
	deltas, newVotes, err := computeDeltas(ctx, f.store.nodesIndices, f.votes, f.balances, newBalances)
	if err != nil {
		return [32]byte{}, errors.Wrap(err, "Could not compute deltas")
	}
	f.votes = newVotes

	if err := f.store.applyWeightChanges(ctx, justifiedEpoch, finalizedEpoch, deltas); err != nil {
		return [32]byte{}, errors.Wrap(err, "Could not apply score changes")
	}
	f.balances = newBalances

	return f.store.head(ctx, justifiedRoot)
}

// ProcessAttestation processes attestation for vote accounting, it iterates around validator indices
// and update their votes accordingly.
func (f *ForkChoice) ProcessAttestation(ctx context.Context, validatorIndices []uint64, blockRoot [32]byte, targetEpoch uint64) {
	ctx, span := trace.StartSpan(ctx, "protoArrayForkChoice.ProcessAttestation")
	defer span.End()
	f.votesLock.Lock()
	defer f.votesLock.Unlock()

	for _, index := range validatorIndices {
		// Validator indices will grow the vote cache.
		for index >= uint64(len(f.votes)) {
			f.votes = append(f.votes, Vote{currentRoot: params.BeaconConfig().ZeroHash, nextRoot: params.BeaconConfig().ZeroHash})
		}

		// Newly allocated vote if the root fields are untouched.
		newVote := f.votes[index].nextRoot == params.BeaconConfig().ZeroHash &&
			f.votes[index].currentRoot == params.BeaconConfig().ZeroHash

		// Vote gets updated if it's newly allocated or high target epoch.
		if newVote || targetEpoch > f.votes[index].nextEpoch {
			f.votes[index].nextEpoch = targetEpoch
			f.votes[index].nextRoot = blockRoot
		}
	}

	processedAttestationCount.Inc()
}

// ProcessBlock processes a new block by inserting it to the fork choice store.
func (f *ForkChoice) ProcessBlock(ctx context.Context, slot uint64, blockRoot, parentRoot, graffiti [32]byte, justifiedEpoch, finalizedEpoch uint64) error {
	ctx, span := trace.StartSpan(ctx, "protoArrayForkChoice.ProcessBlock")
	defer span.End()

	return f.store.insert(ctx, slot, blockRoot, parentRoot, graffiti, justifiedEpoch, finalizedEpoch)
}

// Prune prunes the fork choice store with the new finalized root. The store is only pruned if the input
// root is different than the current store finalized root, and the number of the store has met prune threshold.
func (f *ForkChoice) Prune(ctx context.Context, finalizedRoot [32]byte) error {
	return f.store.prune(ctx, finalizedRoot)
}

// Nodes returns the copied list of block nodes in the fork choice store.
func (f *ForkChoice) Nodes() []*Node {
	f.store.nodesLock.RLock()
	defer f.store.nodesLock.RUnlock()

	cpy := make([]*Node, len(f.store.nodes))
	copy(cpy, f.store.nodes)
	return cpy
}

// Store returns the fork choice store object which contains all the information regarding proto array fork choice.
func (f *ForkChoice) Store() *Store {
	f.store.nodesLock.Lock()
	defer f.store.nodesLock.Unlock()
	return f.store
}

// Node returns the copied node in the fork choice store.
func (f *ForkChoice) Node(root [32]byte) *Node {
	f.store.nodesLock.RLock()
	defer f.store.nodesLock.RUnlock()

	index, ok := f.store.nodesIndices[root]
	if !ok {
		return nil
	}

	return copyNode(f.store.nodes[index])
}

// HasNode returns true if the node exists in fork choice store,
// false else wise.
func (f *ForkChoice) HasNode(root [32]byte) bool {
	f.store.nodesLock.RLock()
	defer f.store.nodesLock.RUnlock()

	_, ok := f.store.nodesIndices[root]
	return ok
}

// HasParent returns true if the node parent exists in fork choice store,
// false else wise.
func (f *ForkChoice) HasParent(root [32]byte) bool {
	f.store.nodesLock.RLock()
	defer f.store.nodesLock.RUnlock()

	i, ok := f.store.nodesIndices[root]
	if !ok || i >= uint64(len(f.store.nodes)) {
		return false
	}

	return f.store.nodes[i].parent != NonExistentNode
}

// IsCanonical returns true if the given root is part of the canonical chain.
func (f *ForkChoice) IsCanonical(root [32]byte) bool {
	f.store.nodesLock.RLock()
	defer f.store.nodesLock.RUnlock()

	return f.store.canonicalNodes[root]
}

// AncestorRoot returns the ancestor root of input block root at a given slot.
func (f *ForkChoice) AncestorRoot(ctx context.Context, root [32]byte, slot uint64) ([]byte, error) {
	ctx, span := trace.StartSpan(ctx, "protoArray.AncestorRoot")
	defer span.End()

	f.store.nodesLock.RLock()
	defer f.store.nodesLock.RUnlock()

	i, ok := f.store.nodesIndices[root]
	if !ok {
		return nil, errors.New("node does not exist")
	}
	if i >= uint64(len(f.store.nodes)) {
		return nil, errors.New("node index out of range")
	}

	for f.store.nodes[i].slot > slot {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		i = f.store.nodes[i].parent

		if i >= uint64(len(f.store.nodes)) {
			return nil, errors.New("node index out of range")
		}
	}

	return f.store.nodes[i].root[:], nil
}

// PruneThreshold of fork choice store.
func (s *Store) PruneThreshold() uint64 {
	return s.pruneThreshold
}

// JustifiedEpoch of fork choice store.
func (s *Store) JustifiedEpoch() uint64 {
	return s.justifiedEpoch
}

// FinalizedEpoch of fork choice store.
func (s *Store) FinalizedEpoch() uint64 {
	return s.finalizedEpoch
}

// Nodes of fork choice store.
func (s *Store) Nodes() []*Node {
	s.nodesLock.RLock()
	defer s.nodesLock.RUnlock()
	return s.nodes
}

// NodesIndices of fork choice store.
func (s *Store) NodesIndices() map[[32]byte]uint64 {
	s.nodesLock.RLock()
	defer s.nodesLock.RUnlock()
	return s.nodesIndices
}

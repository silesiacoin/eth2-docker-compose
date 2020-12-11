package sync

import (
	"context"
	"io"

	streamhelpers "github.com/libp2p/go-libp2p-core/helpers"
	"github.com/libp2p/go-libp2p-core/mux"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/pkg/errors"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/prysm/beacon-chain/p2p"
	"github.com/prysmaticlabs/prysm/beacon-chain/p2p/types"
	pb "github.com/prysmaticlabs/prysm/proto/beacon/p2p/v1"
	"github.com/prysmaticlabs/prysm/shared/params"
)

// ErrInvalidFetchedData is thrown if stream fails to provide requested blocks.
var ErrInvalidFetchedData = errors.New("invalid data returned from peer")

// BeaconBlockProcessor defines a block processing function, which allows to start utilizing
// blocks even before all blocks are ready.
type BeaconBlockProcessor func(block *ethpb.SignedBeaconBlock) error

// SendBeaconBlocksByRangeRequest sends BeaconBlocksByRange and returns fetched blocks, if any.
func SendBeaconBlocksByRangeRequest(
	ctx context.Context, p2pProvider p2p.P2P, pid peer.ID,
	req *pb.BeaconBlocksByRangeRequest, blockProcessor BeaconBlockProcessor,
) ([]*ethpb.SignedBeaconBlock, error) {
	stream, err := p2pProvider.Send(ctx, req, p2p.RPCBlocksByRangeTopic, pid)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := streamhelpers.FullClose(stream); err != nil && err.Error() != mux.ErrReset.Error() {
			log.WithError(err).Debugf("Could not close stream with protocol %s", stream.Protocol())
		}
	}()

	// Augment block processing function, if non-nil block processor is provided.
	blocks := make([]*ethpb.SignedBeaconBlock, 0, req.Count)
	process := func(blk *ethpb.SignedBeaconBlock) error {
		blocks = append(blocks, blk)
		if blockProcessor != nil {
			return blockProcessor(blk)
		}
		return nil
	}
	var prevSlot uint64
	for i := uint64(0); ; i++ {
		isFirstChunk := i == 0
		blk, err := ReadChunkedBlock(stream, p2pProvider, isFirstChunk)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		// The response MUST contain no more than `count` blocks, and no more than
		// MAX_REQUEST_BLOCKS blocks.
		if i >= req.Count || i >= params.BeaconNetworkConfig().MaxRequestBlocks {
			return nil, ErrInvalidFetchedData
		}
		// Returned blocks MUST be in the slot range [start_slot, start_slot + count * step).
		if blk.Block.Slot < req.StartSlot || blk.Block.Slot >= req.StartSlot+req.Count*req.Step {
			return nil, ErrInvalidFetchedData
		}
		// Returned blocks, where they exist, MUST be sent in a consecutive order.
		// Consecutive blocks MUST have values in `step` increments (slots may be skipped in between).
		if !isFirstChunk && (prevSlot >= blk.Block.Slot || (blk.Block.Slot-prevSlot)%req.Step != 0) {
			return nil, ErrInvalidFetchedData
		}
		prevSlot = blk.Block.Slot
		if err := process(blk); err != nil {
			return nil, err
		}
	}
	return blocks, nil
}

// SendBeaconBlocksByRootRequest sends BeaconBlocksByRoot and returns fetched blocks, if any.
func SendBeaconBlocksByRootRequest(
	ctx context.Context, p2pProvider p2p.P2P, pid peer.ID,
	req *types.BeaconBlockByRootsReq, blockProcessor BeaconBlockProcessor,
) ([]*ethpb.SignedBeaconBlock, error) {
	stream, err := p2pProvider.Send(ctx, req, p2p.RPCBlocksByRootTopic, pid)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := streamhelpers.FullClose(stream); err != nil && err.Error() != mux.ErrReset.Error() {
			log.WithError(err).Debugf("Could not reset stream with protocol %s", stream.Protocol())
		}
	}()

	// Augment block processing function, if non-nil block processor is provided.
	blocks := make([]*ethpb.SignedBeaconBlock, 0, len(*req))
	process := func(block *ethpb.SignedBeaconBlock) error {
		blocks = append(blocks, block)
		if blockProcessor != nil {
			return blockProcessor(block)
		}
		return nil
	}
	for i := 0; i < len(*req); i++ {
		// Exit if peer sends more than max request blocks.
		if uint64(i) >= params.BeaconNetworkConfig().MaxRequestBlocks {
			break
		}
		isFirstChunk := i == 0
		blk, err := ReadChunkedBlock(stream, p2pProvider, isFirstChunk)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}

		if err := process(blk); err != nil {
			return nil, err
		}
	}
	return blocks, nil
}

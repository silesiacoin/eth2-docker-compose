package sync

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/kevinms/leakybucket-go"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/protocol"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	chainMock "github.com/prysmaticlabs/prysm/beacon-chain/blockchain/testing"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	db2 "github.com/prysmaticlabs/prysm/beacon-chain/db"
	db "github.com/prysmaticlabs/prysm/beacon-chain/db/testing"
	"github.com/prysmaticlabs/prysm/beacon-chain/flags"
	"github.com/prysmaticlabs/prysm/beacon-chain/p2p/encoder"
	p2ptest "github.com/prysmaticlabs/prysm/beacon-chain/p2p/testing"
	p2ptypes "github.com/prysmaticlabs/prysm/beacon-chain/p2p/types"
	pb "github.com/prysmaticlabs/prysm/proto/beacon/p2p/v1"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
	"github.com/prysmaticlabs/prysm/shared/params"
	"github.com/prysmaticlabs/prysm/shared/testutil"
	"github.com/prysmaticlabs/prysm/shared/testutil/assert"
	"github.com/prysmaticlabs/prysm/shared/testutil/require"
	logTest "github.com/sirupsen/logrus/hooks/test"
)

func TestRPCBeaconBlocksByRange_RPCHandlerReturnsBlocks(t *testing.T) {
	p1 := p2ptest.NewTestP2P(t)
	p2 := p2ptest.NewTestP2P(t)
	p1.Connect(p2)
	assert.Equal(t, 1, len(p1.BHost.Network().Peers()), "Expected peers to be connected")
	d, _ := db.SetupDB(t)

	req := &pb.BeaconBlocksByRangeRequest{
		StartSlot: 100,
		Step:      64,
		Count:     16,
	}

	// Populate the database with blocks that would match the request.
	for i := req.StartSlot; i < req.StartSlot+(req.Step*req.Count); i += req.Step {
		blk := testutil.NewBeaconBlock()
		blk.Block.Slot = i
		require.NoError(t, d.SaveBlock(context.Background(), blk))
	}

	// Start service with 160 as allowed blocks capacity (and almost zero capacity recovery).
	r := &Service{p2p: p1, db: d, chain: &chainMock.ChainService{}, rateLimiter: newRateLimiter(p1)}
	pcl := protocol.ID("/testing")
	topic := string(pcl)
	r.rateLimiter.limiterMap[topic] = leakybucket.NewCollector(0.000001, int64(req.Count*10), false)
	var wg sync.WaitGroup
	wg.Add(1)
	p2.BHost.SetStreamHandler(pcl, func(stream network.Stream) {
		defer wg.Done()
		for i := req.StartSlot; i < req.StartSlot+req.Count*req.Step; i += req.Step {
			expectSuccess(t, stream)
			res := testutil.NewBeaconBlock()
			assert.NoError(t, r.p2p.Encoding().DecodeWithMaxLength(stream, res))
			if (res.Block.Slot-req.StartSlot)%req.Step != 0 {
				t.Errorf("Received unexpected block slot %d", res.Block.Slot)
			}
		}
	})

	stream1, err := p1.BHost.NewStream(context.Background(), p2.BHost.ID(), pcl)
	require.NoError(t, err)

	err = r.beaconBlocksByRangeRPCHandler(context.Background(), req, stream1)
	require.NoError(t, err)

	// Make sure that rate limiter doesn't limit capacity exceedingly.
	remainingCapacity := r.rateLimiter.limiterMap[topic].Remaining(p2.PeerID().String())
	expectedCapacity := int64(req.Count*10 - req.Count)
	require.Equal(t, expectedCapacity, remainingCapacity, "Unexpected rate limiting capacity")

	if testutil.WaitTimeout(&wg, 1*time.Second) {
		t.Fatal("Did not receive stream within 1 sec")
	}
}

func TestRPCBeaconBlocksByRange_ReturnCorrectNumberBack(t *testing.T) {
	p1 := p2ptest.NewTestP2P(t)
	p2 := p2ptest.NewTestP2P(t)
	p1.Connect(p2)
	assert.Equal(t, 1, len(p1.BHost.Network().Peers()), "Expected peers to be connected")
	d, _ := db.SetupDB(t)

	req := &pb.BeaconBlocksByRangeRequest{
		StartSlot: 0,
		Step:      1,
		Count:     200,
	}

	genRoot := [32]byte{}
	// Populate the database with blocks that would match the request.
	for i := req.StartSlot; i < req.StartSlot+(req.Step*req.Count); i += req.Step {
		blk := testutil.NewBeaconBlock()
		blk.Block.Slot = i
		if i == 0 {
			rt, err := blk.Block.HashTreeRoot()
			require.NoError(t, err)
			genRoot = rt
		}
		require.NoError(t, d.SaveBlock(context.Background(), blk))
	}
	require.NoError(t, d.SaveGenesisBlockRoot(context.Background(), genRoot))

	// Start service with 160 as allowed blocks capacity (and almost zero capacity recovery).
	r := &Service{p2p: p1, db: d, chain: &chainMock.ChainService{}, rateLimiter: newRateLimiter(p1)}
	pcl := protocol.ID("/testing")
	topic := string(pcl)
	r.rateLimiter.limiterMap[topic] = leakybucket.NewCollector(0.000001, int64(req.Count*10), false)
	var wg sync.WaitGroup
	wg.Add(1)

	// Use a new request to test this out
	newReq := &pb.BeaconBlocksByRangeRequest{StartSlot: 0, Step: 1, Count: 1}

	p2.BHost.SetStreamHandler(pcl, func(stream network.Stream) {
		defer wg.Done()
		for i := newReq.StartSlot; i < newReq.StartSlot+newReq.Count*newReq.Step; i += newReq.Step {
			expectSuccess(t, stream)
			res := testutil.NewBeaconBlock()
			assert.NoError(t, r.p2p.Encoding().DecodeWithMaxLength(stream, res))
			if (res.Block.Slot-newReq.StartSlot)%newReq.Step != 0 {
				t.Errorf("Received unexpected block slot %d", res.Block.Slot)
			}
			// Expect EOF
			b := make([]byte, 1)
			_, err := stream.Read(b)
			require.ErrorContains(t, io.EOF.Error(), err)
		}
	})

	stream1, err := p1.BHost.NewStream(context.Background(), p2.BHost.ID(), pcl)
	require.NoError(t, err)

	err = r.beaconBlocksByRangeRPCHandler(context.Background(), newReq, stream1)
	require.NoError(t, err)

	if testutil.WaitTimeout(&wg, 1*time.Second) {
		t.Fatal("Did not receive stream within 1 sec")
	}
}

func TestRPCBeaconBlocksByRange_RPCHandlerReturnsSortedBlocks(t *testing.T) {
	p1 := p2ptest.NewTestP2P(t)
	p2 := p2ptest.NewTestP2P(t)
	p1.Connect(p2)
	assert.Equal(t, 1, len(p1.BHost.Network().Peers()), "Expected peers to be connected")
	d, _ := db.SetupDB(t)

	req := &pb.BeaconBlocksByRangeRequest{
		StartSlot: 200,
		Step:      21,
		Count:     33,
	}

	endSlot := req.StartSlot + (req.Step * (req.Count - 1))
	expectedRoots := make([][32]byte, req.Count)
	// Populate the database with blocks that would match the request.
	for i, j := endSlot, req.Count-1; i >= req.StartSlot; i -= req.Step {
		blk := testutil.NewBeaconBlock()
		blk.Block.Slot = i
		rt, err := blk.Block.HashTreeRoot()
		require.NoError(t, err)
		expectedRoots[j] = rt
		require.NoError(t, d.SaveBlock(context.Background(), blk))
		j--
	}

	// Start service with 160 as allowed blocks capacity (and almost zero capacity recovery).
	r := &Service{p2p: p1, db: d, rateLimiter: newRateLimiter(p1),
		chain: &chainMock.ChainService{}}
	pcl := protocol.ID("/testing")
	topic := string(pcl)
	r.rateLimiter.limiterMap[topic] = leakybucket.NewCollector(0.000001, int64(req.Count*10), false)

	var wg sync.WaitGroup
	wg.Add(1)
	p2.BHost.SetStreamHandler(pcl, func(stream network.Stream) {
		defer wg.Done()
		prevSlot := uint64(0)
		require.Equal(t, uint64(len(expectedRoots)), req.Count, "Number of roots not expected")
		for i, j := req.StartSlot, 0; i < req.StartSlot+req.Count*req.Step; i += req.Step {
			expectSuccess(t, stream)
			res := &ethpb.SignedBeaconBlock{}
			assert.NoError(t, r.p2p.Encoding().DecodeWithMaxLength(stream, res))
			if res.Block.Slot < prevSlot {
				t.Errorf("Received block is unsorted with slot %d lower than previous slot %d", res.Block.Slot, prevSlot)
			}
			rt, err := res.Block.HashTreeRoot()
			require.NoError(t, err)
			assert.Equal(t, expectedRoots[j], rt, "roots not equal")
			prevSlot = res.Block.Slot
			j++
		}
	})

	stream1, err := p1.BHost.NewStream(context.Background(), p2.BHost.ID(), pcl)
	require.NoError(t, err)
	require.NoError(t, r.beaconBlocksByRangeRPCHandler(context.Background(), req, stream1))

	if testutil.WaitTimeout(&wg, 1*time.Second) {
		t.Fatal("Did not receive stream within 1 sec")
	}
}

func TestRPCBeaconBlocksByRange_ReturnsGenesisBlock(t *testing.T) {
	p1 := p2ptest.NewTestP2P(t)
	p2 := p2ptest.NewTestP2P(t)
	p1.Connect(p2)
	assert.Equal(t, 1, len(p1.BHost.Network().Peers()), "Expected peers to be connected")
	d, _ := db.SetupDB(t)

	req := &pb.BeaconBlocksByRangeRequest{
		StartSlot: 0,
		Step:      1,
		Count:     4,
	}

	prevRoot := [32]byte{}
	// Populate the database with blocks that would match the request.
	for i := req.StartSlot; i < req.StartSlot+(req.Step*req.Count); i++ {
		blk := testutil.NewBeaconBlock()
		blk.Block.Slot = i
		blk.Block.ParentRoot = prevRoot[:]
		rt, err := blk.Block.HashTreeRoot()
		require.NoError(t, err)

		// Save genesis block
		if i == 0 {
			require.NoError(t, d.SaveGenesisBlockRoot(context.Background(), rt))
		}
		require.NoError(t, d.SaveBlock(context.Background(), blk))
		prevRoot = rt
	}

	r := &Service{p2p: p1, db: d, chain: &chainMock.ChainService{}, rateLimiter: newRateLimiter(p1)}
	pcl := protocol.ID("/testing")
	topic := string(pcl)
	r.rateLimiter.limiterMap[topic] = leakybucket.NewCollector(10000, 10000, false)

	var wg sync.WaitGroup
	wg.Add(1)
	p2.BHost.SetStreamHandler(pcl, func(stream network.Stream) {
		defer wg.Done()
		// check for genesis block
		expectSuccess(t, stream)
		res := &ethpb.SignedBeaconBlock{}
		assert.NoError(t, r.p2p.Encoding().DecodeWithMaxLength(stream, res))
		assert.Equal(t, uint64(0), res.Block.Slot, "genesis block was not returned")
		for i := req.StartSlot + req.Step; i < req.Count*req.Step; i += req.Step {
			expectSuccess(t, stream)
			res := &ethpb.SignedBeaconBlock{}
			assert.NoError(t, r.p2p.Encoding().DecodeWithMaxLength(stream, res))
		}
	})

	stream1, err := p1.BHost.NewStream(context.Background(), p2.BHost.ID(), pcl)
	require.NoError(t, err)
	require.NoError(t, r.beaconBlocksByRangeRPCHandler(context.Background(), req, stream1))

	if testutil.WaitTimeout(&wg, 1*time.Second) {
		t.Fatal("Did not receive stream within 1 sec")
	}
}

func TestRPCBeaconBlocksByRange_RPCHandlerRateLimitOverflow(t *testing.T) {
	d, _ := db.SetupDB(t)
	saveBlocks := func(req *pb.BeaconBlocksByRangeRequest) {
		// Populate the database with blocks that would match the request.
		parentRoot := [32]byte{}
		for i := req.StartSlot; i < req.StartSlot+(req.Step*req.Count); i += req.Step {
			block := testutil.NewBeaconBlock()
			block.Block.Slot = i
			if req.Step == 1 {
				block.Block.ParentRoot = parentRoot[:]
			}
			require.NoError(t, d.SaveBlock(context.Background(), block))
			rt, err := block.Block.HashTreeRoot()
			require.NoError(t, err)
			parentRoot = rt
		}
	}
	sendRequest := func(p1, p2 *p2ptest.TestP2P, r *Service,
		req *pb.BeaconBlocksByRangeRequest, validateBlocks bool, success bool) error {
		var wg sync.WaitGroup
		wg.Add(1)
		pcl := protocol.ID("/testing")
		p2.BHost.SetStreamHandler(pcl, func(stream network.Stream) {
			defer wg.Done()
			if !validateBlocks {
				return
			}
			for i := req.StartSlot; i < req.StartSlot+req.Count*req.Step; i += req.Step {
				if !success {
					continue
				}
				expectSuccess(t, stream)
				res := testutil.NewBeaconBlock()
				assert.NoError(t, r.p2p.Encoding().DecodeWithMaxLength(stream, res))
				if (res.Block.Slot-req.StartSlot)%req.Step != 0 {
					t.Errorf("Received unexpected block slot %d", res.Block.Slot)
				}
			}
		})
		stream, err := p1.BHost.NewStream(context.Background(), p2.BHost.ID(), pcl)
		require.NoError(t, err)
		if err = r.beaconBlocksByRangeRPCHandler(context.Background(), req, stream); err != nil {
			return err
		}
		if testutil.WaitTimeout(&wg, 1*time.Second) {
			t.Fatal("Did not receive stream within 1 sec")
		}
		return nil
	}

	t.Run("high request count param and no overflow", func(t *testing.T) {
		p1 := p2ptest.NewTestP2P(t)
		p2 := p2ptest.NewTestP2P(t)
		p1.Connect(p2)
		assert.Equal(t, 1, len(p1.BHost.Network().Peers()), "Expected peers to be connected")

		capacity := int64(flags.Get().BlockBatchLimit * 3)
		r := &Service{p2p: p1, db: d, chain: &chainMock.ChainService{}, rateLimiter: newRateLimiter(p1)}

		pcl := protocol.ID("/testing")
		topic := string(pcl)
		r.rateLimiter.limiterMap[topic] = leakybucket.NewCollector(0.000001, capacity, false)
		req := &pb.BeaconBlocksByRangeRequest{
			StartSlot: 100,
			Step:      5,
			Count:     uint64(capacity),
		}
		saveBlocks(req)

		assert.NoError(t, sendRequest(p1, p2, r, req, true, true))

		remainingCapacity := r.rateLimiter.limiterMap[topic].Remaining(p2.PeerID().String())
		expectedCapacity := int64(0) // Whole capacity is used, but no overflow.
		assert.Equal(t, expectedCapacity, remainingCapacity, "Unexpected rate limiting capacity")
	})

	t.Run("high request count param and overflow", func(t *testing.T) {
		p1 := p2ptest.NewTestP2P(t)
		p2 := p2ptest.NewTestP2P(t)
		p1.Connect(p2)
		assert.Equal(t, 1, len(p1.BHost.Network().Peers()), "Expected peers to be connected")

		capacity := int64(flags.Get().BlockBatchLimit * 3)
		r := &Service{p2p: p1, db: d, chain: &chainMock.ChainService{}, rateLimiter: newRateLimiter(p1)}

		pcl := protocol.ID("/testing")
		topic := string(pcl)
		r.rateLimiter.limiterMap[topic] = leakybucket.NewCollector(0.000001, capacity, false)

		req := &pb.BeaconBlocksByRangeRequest{
			StartSlot: 100,
			Step:      5,
			Count:     uint64(capacity + 1),
		}
		saveBlocks(req)

		for i := 0; i < p2.Peers().Scorers().BadResponsesScorer().Params().Threshold; i++ {
			err := sendRequest(p1, p2, r, req, false, true)
			assert.ErrorContains(t, p2ptypes.ErrRateLimited.Error(), err)
		}

		remainingCapacity := r.rateLimiter.limiterMap[topic].Remaining(p2.PeerID().String())
		expectedCapacity := int64(0) // Whole capacity is used.
		assert.Equal(t, expectedCapacity, remainingCapacity, "Unexpected rate limiting capacity")
	})

	t.Run("many requests with count set to max blocks per second", func(t *testing.T) {
		p1 := p2ptest.NewTestP2P(t)
		p2 := p2ptest.NewTestP2P(t)
		p1.Connect(p2)
		assert.Equal(t, 1, len(p1.BHost.Network().Peers()), "Expected peers to be connected")

		capacity := int64(flags.Get().BlockBatchLimit * flags.Get().BlockBatchLimitBurstFactor)
		r := &Service{p2p: p1, db: d, chain: &chainMock.ChainService{}, rateLimiter: newRateLimiter(p1)}
		pcl := protocol.ID("/testing")
		topic := string(pcl)
		r.rateLimiter.limiterMap[topic] = leakybucket.NewCollector(0.000001, capacity, false)

		req := &pb.BeaconBlocksByRangeRequest{
			StartSlot: 100,
			Step:      1,
			Count:     uint64(flags.Get().BlockBatchLimit),
		}
		saveBlocks(req)

		for i := 0; i < flags.Get().BlockBatchLimitBurstFactor; i++ {
			assert.NoError(t, sendRequest(p1, p2, r, req, true, false))
		}

		// One more request should result in overflow.
		for i := 0; i < p2.Peers().Scorers().BadResponsesScorer().Params().Threshold; i++ {
			err := sendRequest(p1, p2, r, req, false, false)
			assert.ErrorContains(t, p2ptypes.ErrRateLimited.Error(), err)
		}

		remainingCapacity := r.rateLimiter.limiterMap[topic].Remaining(p2.PeerID().String())
		expectedCapacity := int64(0) // Whole capacity is used.
		assert.Equal(t, expectedCapacity, remainingCapacity, "Unexpected rate limiting capacity")
	})
}

func TestRPCBeaconBlocksByRange_validateRangeRequest(t *testing.T) {
	slotsSinceGenesis := 1000
	r := &Service{chain: &chainMock.ChainService{
		Genesis: time.Now().Add(time.Second * time.Duration(-slotsSinceGenesis*int(params.BeaconConfig().SecondsPerSlot))),
	}}

	tests := []struct {
		name          string
		req           *pb.BeaconBlocksByRangeRequest
		expectedError error
		errorToLog    string
	}{
		{
			name: "Zero Count",
			req: &pb.BeaconBlocksByRangeRequest{
				Count: 0,
				Step:  1,
			},
			expectedError: p2ptypes.ErrInvalidRequest,
			errorToLog:    "validation did not fail with bad count",
		},
		{
			name: "Over limit Count",
			req: &pb.BeaconBlocksByRangeRequest{
				Count: params.BeaconNetworkConfig().MaxRequestBlocks + 1,
				Step:  1,
			},
			expectedError: p2ptypes.ErrInvalidRequest,
			errorToLog:    "validation did not fail with bad count",
		},
		{
			name: "Correct Count",
			req: &pb.BeaconBlocksByRangeRequest{
				Count: params.BeaconNetworkConfig().MaxRequestBlocks - 1,
				Step:  1,
			},
			errorToLog: "validation failed with correct count",
		},
		{
			name: "Zero Step",
			req: &pb.BeaconBlocksByRangeRequest{
				Step:  0,
				Count: 1,
			},
			expectedError: p2ptypes.ErrInvalidRequest,
			errorToLog:    "validation did not fail with bad step",
		},
		{
			name: "Over limit Step",
			req: &pb.BeaconBlocksByRangeRequest{
				Step:  rangeLimit + 1,
				Count: 1,
			},
			expectedError: p2ptypes.ErrInvalidRequest,
			errorToLog:    "validation did not fail with bad step",
		},
		{
			name: "Correct Step",
			req: &pb.BeaconBlocksByRangeRequest{
				Step:  rangeLimit - 1,
				Count: 2,
			},
			errorToLog: "validation failed with correct step",
		},
		{
			name: "Over Limit Start Slot",
			req: &pb.BeaconBlocksByRangeRequest{
				StartSlot: uint64(slotsSinceGenesis) + (2 * rangeLimit) + 1,
				Step:      1,
				Count:     1,
			},
			expectedError: p2ptypes.ErrInvalidRequest,
			errorToLog:    "validation did not fail with bad start slot",
		},
		{
			name: "Over Limit End Slot",
			req: &pb.BeaconBlocksByRangeRequest{
				Step:  1,
				Count: params.BeaconNetworkConfig().MaxRequestBlocks + 1,
			},
			expectedError: p2ptypes.ErrInvalidRequest,
			errorToLog:    "validation did not fail with bad end slot",
		},
		{
			name: "Exceed Range Limit",
			req: &pb.BeaconBlocksByRangeRequest{
				Step:  3,
				Count: uint64(slotsSinceGenesis / 2),
			},
			expectedError: p2ptypes.ErrInvalidRequest,
			errorToLog:    "validation did not fail with bad range",
		},
		{
			name: "Valid Request",
			req: &pb.BeaconBlocksByRangeRequest{
				Step:      1,
				Count:     params.BeaconNetworkConfig().MaxRequestBlocks - 1,
				StartSlot: 50,
			},
			errorToLog: "validation failed with valid params",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.expectedError != nil {
				assert.ErrorContains(t, tt.expectedError.Error(), r.validateRangeRequest(tt.req), tt.errorToLog)
			} else {
				assert.NoError(t, r.validateRangeRequest(tt.req), tt.errorToLog)
			}
		})
	}
}

func TestRPCBeaconBlocksByRange_EnforceResponseInvariants(t *testing.T) {
	d, _ := db.SetupDB(t)
	hook := logTest.NewGlobal()
	saveBlocks := func(req *pb.BeaconBlocksByRangeRequest) {
		// Populate the database with blocks that would match the request.
		parentRoot := [32]byte{}
		for i := req.StartSlot; i < req.StartSlot+(req.Step*req.Count); i += req.Step {
			block := testutil.NewBeaconBlock()
			block.Block.Slot = i
			block.Block.ParentRoot = parentRoot[:]
			require.NoError(t, d.SaveBlock(context.Background(), block))
			rt, err := block.Block.HashTreeRoot()
			require.NoError(t, err)
			parentRoot = rt
		}
	}
	pcl := protocol.ID("/testing")
	sendRequest := func(p1, p2 *p2ptest.TestP2P, r *Service,
		req *pb.BeaconBlocksByRangeRequest, processBlocks func([]*ethpb.SignedBeaconBlock)) error {
		var wg sync.WaitGroup
		wg.Add(1)
		p2.BHost.SetStreamHandler(pcl, func(stream network.Stream) {
			defer wg.Done()
			blocks := make([]*ethpb.SignedBeaconBlock, 0, req.Count)
			for i := req.StartSlot; i < req.StartSlot+req.Count*req.Step; i += req.Step {
				expectSuccess(t, stream)
				blk := testutil.NewBeaconBlock()
				assert.NoError(t, r.p2p.Encoding().DecodeWithMaxLength(stream, blk))
				if (blk.Block.Slot-req.StartSlot)%req.Step != 0 {
					t.Errorf("Received unexpected block slot %d", blk.Block.Slot)
				}
				blocks = append(blocks, blk)
			}
			processBlocks(blocks)
		})
		stream, err := p1.BHost.NewStream(context.Background(), p2.BHost.ID(), pcl)
		require.NoError(t, err)
		if err = r.beaconBlocksByRangeRPCHandler(context.Background(), req, stream); err != nil {
			return err
		}
		if testutil.WaitTimeout(&wg, 1*time.Second) {
			t.Fatal("Did not receive stream within 1 sec")
		}
		return nil
	}

	t.Run("assert range", func(t *testing.T) {
		p1 := p2ptest.NewTestP2P(t)
		p2 := p2ptest.NewTestP2P(t)
		p1.Connect(p2)
		assert.Equal(t, 1, len(p1.BHost.Network().Peers()), "Expected peers to be connected")

		r := &Service{p2p: p1, db: d, chain: &chainMock.ChainService{}, rateLimiter: newRateLimiter(p1)}
		r.rateLimiter.limiterMap[string(pcl)] = leakybucket.NewCollector(0.000001, 640, false)
		req := &pb.BeaconBlocksByRangeRequest{
			StartSlot: 448,
			Step:      1,
			Count:     64,
		}
		saveBlocks(req)

		hook.Reset()
		err := sendRequest(p1, p2, r, req, func(blocks []*ethpb.SignedBeaconBlock) {
			assert.Equal(t, req.Count, uint64(len(blocks)))
			for _, blk := range blocks {
				if blk.Block.Slot < req.StartSlot || blk.Block.Slot >= req.StartSlot+req.Count*req.Step {
					t.Errorf("Block slot is out of range: %d is not within [%d, %d)",
						blk.Block.Slot, req.StartSlot, req.StartSlot+req.Count*req.Step)
				}
			}
		})
		assert.NoError(t, err)
		require.LogsDoNotContain(t, hook, "Disconnecting bad peer")
	})
}

func TestRPCBeaconBlocksByRange_FilterBlocks(t *testing.T) {
	hook := logTest.NewGlobal()

	saveBlocks := func(d db2.Database, chain *chainMock.ChainService, req *pb.BeaconBlocksByRangeRequest, finalized bool) {
		blk := testutil.NewBeaconBlock()
		blk.Block.Slot = 0
		previousRoot, err := blk.Block.HashTreeRoot()
		require.NoError(t, err)

		require.NoError(t, d.SaveBlock(context.Background(), blk))
		require.NoError(t, d.SaveGenesisBlockRoot(context.Background(), previousRoot))
		blocks := make([]*ethpb.SignedBeaconBlock, req.Count)
		// Populate the database with blocks that would match the request.
		for i, j := req.StartSlot, 0; i < req.StartSlot+(req.Step*req.Count); i += req.Step {
			parentRoot := make([]byte, 32)
			copy(parentRoot, previousRoot[:])
			blocks[j] = testutil.NewBeaconBlock()
			blocks[j].Block.Slot = i
			blocks[j].Block.ParentRoot = parentRoot
			var err error
			previousRoot, err = blocks[j].Block.HashTreeRoot()
			require.NoError(t, err)
			require.NoError(t, d.SaveBlock(context.Background(), blocks[j]))
			j++
		}
		stateSummaries := make([]*pb.StateSummary, len(blocks))

		if finalized {
			if chain.CanonicalRoots == nil {
				chain.CanonicalRoots = map[[32]byte]bool{}
			}
			for i, b := range blocks {
				bRoot, err := b.Block.HashTreeRoot()
				require.NoError(t, err)
				stateSummaries[i] = &pb.StateSummary{
					Slot: b.Block.Slot,
					Root: bRoot[:],
				}
				chain.CanonicalRoots[bRoot] = true
			}
			require.NoError(t, d.SaveStateSummaries(context.Background(), stateSummaries))
			require.NoError(t, d.SaveFinalizedCheckpoint(context.Background(), &ethpb.Checkpoint{
				Epoch: helpers.SlotToEpoch(stateSummaries[len(stateSummaries)-1].Slot),
				Root:  stateSummaries[len(stateSummaries)-1].Root,
			}))
		}
	}
	saveBadBlocks := func(d db2.Database, chain *chainMock.ChainService,
		req *pb.BeaconBlocksByRangeRequest, badBlockNum uint64, finalized bool) {
		blk := testutil.NewBeaconBlock()
		blk.Block.Slot = 0
		previousRoot, err := blk.Block.HashTreeRoot()
		require.NoError(t, err)
		genRoot := previousRoot

		require.NoError(t, d.SaveBlock(context.Background(), blk))
		require.NoError(t, d.SaveGenesisBlockRoot(context.Background(), previousRoot))
		blocks := make([]*ethpb.SignedBeaconBlock, req.Count)
		// Populate the database with blocks with non linear roots.
		for i, j := req.StartSlot, 0; i < req.StartSlot+(req.Step*req.Count); i += req.Step {
			parentRoot := make([]byte, 32)
			copy(parentRoot, previousRoot[:])
			blocks[j] = testutil.NewBeaconBlock()
			blocks[j].Block.Slot = i
			blocks[j].Block.ParentRoot = parentRoot
			// Make the 2nd block have a bad root.
			if j == int(badBlockNum) {
				blocks[j].Block.ParentRoot = genRoot[:]
			}
			var err error
			previousRoot, err = blocks[j].Block.HashTreeRoot()
			require.NoError(t, err)
			require.NoError(t, d.SaveBlock(context.Background(), blocks[j]))
			j++
		}
		stateSummaries := make([]*pb.StateSummary, len(blocks))
		if finalized {
			if chain.CanonicalRoots == nil {
				chain.CanonicalRoots = map[[32]byte]bool{}
			}
			for i, b := range blocks {
				bRoot, err := b.Block.HashTreeRoot()
				require.NoError(t, err)
				stateSummaries[i] = &pb.StateSummary{
					Slot: b.Block.Slot,
					Root: bRoot[:],
				}
				chain.CanonicalRoots[bRoot] = true
			}
			require.NoError(t, d.SaveStateSummaries(context.Background(), stateSummaries))
			require.NoError(t, d.SaveFinalizedCheckpoint(context.Background(), &ethpb.Checkpoint{
				Epoch: helpers.SlotToEpoch(stateSummaries[len(stateSummaries)-1].Slot),
				Root:  stateSummaries[len(stateSummaries)-1].Root,
			}))
		}
	}
	pcl := protocol.ID("/testing")
	sendRequest := func(p1, p2 *p2ptest.TestP2P, r *Service,
		req *pb.BeaconBlocksByRangeRequest, processBlocks func([]*ethpb.SignedBeaconBlock)) error {
		var wg sync.WaitGroup
		wg.Add(1)
		p2.BHost.SetStreamHandler(pcl, func(stream network.Stream) {
			defer wg.Done()
			blocks := make([]*ethpb.SignedBeaconBlock, 0, req.Count)
			for i := req.StartSlot; i < req.StartSlot+req.Count*req.Step; i += req.Step {
				code, _, err := ReadStatusCode(stream, &encoder.SszNetworkEncoder{})
				if err != nil && err != io.EOF {
					t.Fatal(err)
				}
				if code != 0 || err == io.EOF {
					break
				}
				blk := testutil.NewBeaconBlock()
				assert.NoError(t, r.p2p.Encoding().DecodeWithMaxLength(stream, blk))
				if (blk.Block.Slot-req.StartSlot)%req.Step != 0 {
					t.Errorf("Received unexpected block slot %d", blk.Block.Slot)
				}
				blocks = append(blocks, blk)
			}
			processBlocks(blocks)
		})
		stream, err := p1.BHost.NewStream(context.Background(), p2.BHost.ID(), pcl)
		require.NoError(t, err)
		if err = r.beaconBlocksByRangeRPCHandler(context.Background(), req, stream); err != nil {
			return err
		}
		if testutil.WaitTimeout(&wg, 1*time.Second) {
			t.Fatal("Did not receive stream within 1 sec")
		}
		return nil
	}

	t.Run("process normal range", func(t *testing.T) {
		p1 := p2ptest.NewTestP2P(t)
		p2 := p2ptest.NewTestP2P(t)
		d, _ := db.SetupDB(t)

		p1.Connect(p2)
		assert.Equal(t, 1, len(p1.BHost.Network().Peers()), "Expected peers to be connected")

		r := &Service{p2p: p1, db: d, chain: &chainMock.ChainService{}, rateLimiter: newRateLimiter(p1)}
		r.rateLimiter.limiterMap[string(pcl)] = leakybucket.NewCollector(0.000001, 640, false)
		req := &pb.BeaconBlocksByRangeRequest{
			StartSlot: 1,
			Step:      1,
			Count:     64,
		}
		saveBlocks(d, r.chain.(*chainMock.ChainService), req, true)

		hook.Reset()
		err := sendRequest(p1, p2, r, req, func(blocks []*ethpb.SignedBeaconBlock) {
			assert.Equal(t, req.Count, uint64(len(blocks)))
			for _, blk := range blocks {
				if blk.Block.Slot < req.StartSlot || blk.Block.Slot >= req.StartSlot+req.Count*req.Step {
					t.Errorf("Block slot is out of range: %d is not within [%d, %d)",
						blk.Block.Slot, req.StartSlot, req.StartSlot+req.Count*req.Step)
				}
			}
		})
		assert.NoError(t, err)
		require.LogsDoNotContain(t, hook, "Disconnecting bad peer")
	})

	t.Run("process non linear blocks", func(t *testing.T) {
		p1 := p2ptest.NewTestP2P(t)
		p2 := p2ptest.NewTestP2P(t)
		d, _ := db.SetupDB(t)

		p1.Connect(p2)
		assert.Equal(t, 1, len(p1.BHost.Network().Peers()), "Expected peers to be connected")

		r := &Service{p2p: p1, db: d, chain: &chainMock.ChainService{}, rateLimiter: newRateLimiter(p1)}
		r.rateLimiter.limiterMap[string(pcl)] = leakybucket.NewCollector(0.000001, 640, false)
		req := &pb.BeaconBlocksByRangeRequest{
			StartSlot: 1,
			Step:      1,
			Count:     64,
		}
		saveBadBlocks(d, r.chain.(*chainMock.ChainService), req, 2, true)

		hook.Reset()
		err := sendRequest(p1, p2, r, req, func(blocks []*ethpb.SignedBeaconBlock) {
			assert.Equal(t, uint64(2), uint64(len(blocks)))
			prevRoot := [32]byte{}
			for _, blk := range blocks {
				if blk.Block.Slot < req.StartSlot || blk.Block.Slot >= req.StartSlot+req.Count*req.Step {
					t.Errorf("Block slot is out of range: %d is not within [%d, %d)",
						blk.Block.Slot, req.StartSlot, req.StartSlot+req.Count*req.Step)
				}
				if prevRoot != [32]byte{} && bytesutil.ToBytes32(blk.Block.ParentRoot) != prevRoot {
					t.Errorf("non linear chain received, expected %#x but got %#x", prevRoot, blk.Block.ParentRoot)
				}
			}
		})
		assert.NoError(t, err)
		require.LogsDoNotContain(t, hook, "Disconnecting bad peer")
	})

	t.Run("process non linear blocks with 2nd bad batch", func(t *testing.T) {
		p1 := p2ptest.NewTestP2P(t)
		p2 := p2ptest.NewTestP2P(t)
		d, _ := db.SetupDB(t)

		p1.Connect(p2)
		assert.Equal(t, 1, len(p1.BHost.Network().Peers()), "Expected peers to be connected")

		r := &Service{p2p: p1, db: d, chain: &chainMock.ChainService{}, rateLimiter: newRateLimiter(p1)}
		r.rateLimiter.limiterMap[string(pcl)] = leakybucket.NewCollector(0.000001, 640, false)
		req := &pb.BeaconBlocksByRangeRequest{
			StartSlot: 1,
			Step:      1,
			Count:     128,
		}
		saveBadBlocks(d, r.chain.(*chainMock.ChainService), req, 65, true)

		hook.Reset()
		err := sendRequest(p1, p2, r, req, func(blocks []*ethpb.SignedBeaconBlock) {
			assert.Equal(t, uint64(65), uint64(len(blocks)))
			prevRoot := [32]byte{}
			for _, blk := range blocks {
				if blk.Block.Slot < req.StartSlot || blk.Block.Slot >= req.StartSlot+req.Count*req.Step {
					t.Errorf("Block slot is out of range: %d is not within [%d, %d)",
						blk.Block.Slot, req.StartSlot, req.StartSlot+req.Count*req.Step)
				}
				if prevRoot != [32]byte{} && bytesutil.ToBytes32(blk.Block.ParentRoot) != prevRoot {
					t.Errorf("non linear chain received, expected %#x but got %#x", prevRoot, blk.Block.ParentRoot)
				}
			}
		})
		assert.NoError(t, err)
		require.LogsDoNotContain(t, hook, "Disconnecting bad peer")
	})

	t.Run("only return finalized blocks", func(t *testing.T) {
		p1 := p2ptest.NewTestP2P(t)
		p2 := p2ptest.NewTestP2P(t)
		d, _ := db.SetupDB(t)

		p1.Connect(p2)
		assert.Equal(t, 1, len(p1.BHost.Network().Peers()), "Expected peers to be connected")

		r := &Service{p2p: p1, db: d, chain: &chainMock.ChainService{}, rateLimiter: newRateLimiter(p1)}
		r.rateLimiter.limiterMap[string(pcl)] = leakybucket.NewCollector(0.000001, 640, false)
		req := &pb.BeaconBlocksByRangeRequest{
			StartSlot: 1,
			Step:      1,
			Count:     64,
		}
		saveBlocks(d, r.chain.(*chainMock.ChainService), req, true)
		req.StartSlot = 65
		req.Step = 1
		req.Count = 128
		// Save unfinalized chain.
		saveBlocks(d, r.chain.(*chainMock.ChainService), req, false)

		req.StartSlot = 1
		hook.Reset()
		err := sendRequest(p1, p2, r, req, func(blocks []*ethpb.SignedBeaconBlock) {
			assert.Equal(t, uint64(64), uint64(len(blocks)))
			prevRoot := [32]byte{}
			for _, blk := range blocks {
				if blk.Block.Slot < req.StartSlot || blk.Block.Slot >= 65 {
					t.Errorf("Block slot is out of range: %d is not within [%d, 64)",
						blk.Block.Slot, req.StartSlot)
				}
				if prevRoot != [32]byte{} && bytesutil.ToBytes32(blk.Block.ParentRoot) != prevRoot {
					t.Errorf("non linear chain received, expected %#x but got %#x", prevRoot, blk.Block.ParentRoot)
				}
			}
		})
		assert.NoError(t, err)
		require.LogsDoNotContain(t, hook, "Disconnecting bad peer")
	})
	t.Run("reject duplicate and non canonical blocks", func(t *testing.T) {
		p1 := p2ptest.NewTestP2P(t)
		p2 := p2ptest.NewTestP2P(t)
		d, _ := db.SetupDB(t)

		p1.Connect(p2)
		assert.Equal(t, 1, len(p1.BHost.Network().Peers()), "Expected peers to be connected")

		r := &Service{p2p: p1, db: d, chain: &chainMock.ChainService{}, rateLimiter: newRateLimiter(p1)}
		r.rateLimiter.limiterMap[string(pcl)] = leakybucket.NewCollector(0.000001, 640, false)
		req := &pb.BeaconBlocksByRangeRequest{
			StartSlot: 1,
			Step:      1,
			Count:     64,
		}
		saveBlocks(d, r.chain.(*chainMock.ChainService), req, true)

		// Create a duplicate set of unfinalized blocks.
		req.StartSlot = 1
		req.Step = 1
		req.Count = 300
		// Save unfinalized chain.
		saveBlocks(d, r.chain.(*chainMock.ChainService), req, false)

		req.Count = 64
		hook.Reset()
		err := sendRequest(p1, p2, r, req, func(blocks []*ethpb.SignedBeaconBlock) {
			assert.Equal(t, uint64(64), uint64(len(blocks)))
			prevRoot := [32]byte{}
			for _, blk := range blocks {
				if blk.Block.Slot < req.StartSlot || blk.Block.Slot >= 65 {
					t.Errorf("Block slot is out of range: %d is not within [%d, 64)",
						blk.Block.Slot, req.StartSlot)
				}
				if prevRoot != [32]byte{} && bytesutil.ToBytes32(blk.Block.ParentRoot) != prevRoot {
					t.Errorf("non linear chain received, expected %#x but got %#x", prevRoot, blk.Block.ParentRoot)
				}
			}
		})
		assert.NoError(t, err)
		require.LogsDoNotContain(t, hook, "Disconnecting bad peer")
	})
}
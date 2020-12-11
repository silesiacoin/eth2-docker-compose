// Package kv includes a key-value store implementation
// of an attestation cache used to satisfy important use-cases
// such as aggregation in a beacon node runtime.
package kv

import (
	"sync"
	"time"

	"github.com/patrickmn/go-cache"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/prysm/shared/hashutil"
	"github.com/prysmaticlabs/prysm/shared/params"
)

var hashFn = hashutil.HashProto

// AttCaches defines the caches used to satisfy attestation pool interface.
// These caches are KV store for various attestations
// such are unaggregated, aggregated or attestations within a block.
type AttCaches struct {
	aggregatedAttLock  sync.RWMutex
	aggregatedAtt      map[[32]byte][]*ethpb.Attestation
	unAggregateAttLock sync.RWMutex
	unAggregatedAtt    map[[32]byte]*ethpb.Attestation
	forkchoiceAttLock  sync.RWMutex
	forkchoiceAtt      map[[32]byte]*ethpb.Attestation
	blockAttLock       sync.RWMutex
	blockAtt           map[[32]byte][]*ethpb.Attestation
	seenAtt            *cache.Cache
}

// NewAttCaches initializes a new attestation pool consists of multiple KV store in cache for
// various kind of attestations.
func NewAttCaches() *AttCaches {
	secsInEpoch := time.Duration(params.BeaconConfig().SlotsPerEpoch * params.BeaconConfig().SecondsPerSlot)
	c := cache.New(secsInEpoch*time.Second, 2*secsInEpoch*time.Second)
	pool := &AttCaches{
		unAggregatedAtt: make(map[[32]byte]*ethpb.Attestation),
		aggregatedAtt:   make(map[[32]byte][]*ethpb.Attestation),
		forkchoiceAtt:   make(map[[32]byte]*ethpb.Attestation),
		blockAtt:        make(map[[32]byte][]*ethpb.Attestation),
		seenAtt:         c,
	}

	return pool
}
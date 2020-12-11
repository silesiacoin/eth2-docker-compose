package db

import "github.com/prysmaticlabs/prysm/beacon-chain/db/iface"

// ReadOnlyDatabase exposes Prysm's eth2 data backend for read access only, no information about
// head info. For head info, use github.com/prysmaticlabs/prysm/blockchain.HeadFetcher.
type ReadOnlyDatabase = iface.ReadOnlyDatabase

// NoHeadAccessDatabase exposes Prysm's eth2 data backend for read/write access, no information
// about head info. For head info, use github.com/prysmaticlabs/prysm/blockchain.HeadFetcher.
type NoHeadAccessDatabase = iface.NoHeadAccessDatabase

// HeadAccessDatabase exposes Prysm's eth2 backend for read/write access with information about
// chain head information. This interface should be used sparingly as the HeadFetcher is the source
// of truth around chain head information while this interface serves as persistent storage for the
// head fetcher.
//
// See github.com/prysmaticlabs/prysm/blockchain.HeadFetcher
type HeadAccessDatabase = iface.HeadAccessDatabase

// Database defines the necessary methods for Prysm's eth2 backend which may be implemented by any
// key-value or relational database in practice. This is the full database interface which should
// not be used often. Prefer a more restrictive interface in this package.
type Database = iface.Database

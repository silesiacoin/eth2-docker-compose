// Package initialsync includes all initial block download and processing
// logic for the beacon node, using a round robin strategy and a finite-state-machine
// to handle edge-cases in a beacon node's sync status.
package initialsync

import (
	"context"
	"time"

	"github.com/paulbellamy/ratecounter"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/beacon-chain/blockchain"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/feed"
	blockfeed "github.com/prysmaticlabs/prysm/beacon-chain/core/feed/block"
	statefeed "github.com/prysmaticlabs/prysm/beacon-chain/core/feed/state"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	"github.com/prysmaticlabs/prysm/beacon-chain/db"
	"github.com/prysmaticlabs/prysm/beacon-chain/flags"
	"github.com/prysmaticlabs/prysm/beacon-chain/p2p"
	"github.com/prysmaticlabs/prysm/shared"
	"github.com/prysmaticlabs/prysm/shared/abool"
	"github.com/prysmaticlabs/prysm/shared/params"
	"github.com/prysmaticlabs/prysm/shared/timeutils"
	"github.com/sirupsen/logrus"
)

var _ shared.Service = (*Service)(nil)

// blockchainService defines the interface for interaction with block chain service.
type blockchainService interface {
	blockchain.BlockReceiver
	blockchain.HeadFetcher
	blockchain.FinalizationFetcher
}

// Config to set up the initial sync service.
type Config struct {
	P2P           p2p.P2P
	DB            db.ReadOnlyDatabase
	Chain         blockchainService
	StateNotifier statefeed.Notifier
	BlockNotifier blockfeed.Notifier
}

// Service service.
type Service struct {
	ctx           context.Context
	cancel        context.CancelFunc
	chain         blockchainService
	p2p           p2p.P2P
	db            db.ReadOnlyDatabase
	synced        *abool.AtomicBool
	chainStarted  *abool.AtomicBool
	stateNotifier statefeed.Notifier
	counter       *ratecounter.RateCounter
	genesisChan   chan time.Time
}

// NewService configures the initial sync service responsible for bringing the node up to the
// latest head of the blockchain.
func NewService(ctx context.Context, cfg *Config) *Service {
	ctx, cancel := context.WithCancel(ctx)
	s := &Service{
		ctx:           ctx,
		cancel:        cancel,
		chain:         cfg.Chain,
		p2p:           cfg.P2P,
		db:            cfg.DB,
		synced:        abool.New(),
		chainStarted:  abool.New(),
		stateNotifier: cfg.StateNotifier,
		counter:       ratecounter.NewRateCounter(counterSeconds * time.Second),
		genesisChan:   make(chan time.Time),
	}
	go s.waitForStateInitialization()
	return s
}

// Start the initial sync service.
func (s *Service) Start() {
	// Wait for state initialized event.
	genesis := <-s.genesisChan
	if genesis.IsZero() {
		log.Debug("Exiting Initial Sync Service")
		return
	}
	if flags.Get().DisableSync {
		s.markSynced(genesis)
		log.WithField("genesisTime", genesis).Info("Due to Sync Being Disabled, entering regular sync immediately.")
		return
	}
	if genesis.After(timeutils.Now()) {
		s.markSynced(genesis)
		log.WithField("genesisTime", genesis).Info("Genesis time has not arrived - not syncing")
		return
	}
	currentSlot := helpers.SlotsSince(genesis)
	if helpers.SlotToEpoch(currentSlot) == 0 {
		log.WithField("genesisTime", genesis).Info("Chain started within the last epoch - not syncing")
		s.markSynced(genesis)
		return
	}
	s.chainStarted.Set()
	log.Info("Starting initial chain sync...")
	// Are we already in sync, or close to it?
	if helpers.SlotToEpoch(s.chain.HeadSlot()) == helpers.SlotToEpoch(currentSlot) {
		log.Info("Already synced to the current chain head")
		s.markSynced(genesis)
		return
	}
	s.waitForMinimumPeers()
	if err := s.roundRobinSync(genesis); err != nil {
		if errors.Is(s.ctx.Err(), context.Canceled) {
			return
		}
		panic(err)
	}
	log.Infof("Synced up to slot %d", s.chain.HeadSlot())
	s.markSynced(genesis)
}

// Stop initial sync.
func (s *Service) Stop() error {
	s.cancel()
	return nil
}

// Status of initial sync.
func (s *Service) Status() error {
	if s.synced.IsNotSet() && s.chainStarted.IsSet() {
		return errors.New("syncing")
	}
	return nil
}

// Syncing returns true if initial sync is still running.
func (s *Service) Syncing() bool {
	return s.synced.IsNotSet()
}

// Resync allows a node to start syncing again if it has fallen
// behind the current network head.
func (s *Service) Resync() error {
	headState, err := s.chain.HeadState(s.ctx)
	if err != nil || headState == nil {
		return errors.Errorf("could not retrieve head state: %v", err)
	}

	// Set it to false since we are syncing again.
	s.synced.UnSet()
	defer func() { s.synced.Set() }() // Reset it at the end of the method.
	genesis := time.Unix(int64(headState.GenesisTime()), 0)

	s.waitForMinimumPeers()
	if err = s.roundRobinSync(genesis); err != nil {
		log = log.WithError(err)
	}
	log.WithField("slot", s.chain.HeadSlot()).Info("Resync attempt complete")
	return nil
}

func (s *Service) waitForMinimumPeers() {
	required := params.BeaconConfig().MaxPeersToSync
	if flags.Get().MinimumSyncPeers < required {
		required = flags.Get().MinimumSyncPeers
	}
	for {
		_, peers := s.p2p.Peers().BestNonFinalized(flags.Get().MinimumSyncPeers, s.chain.FinalizedCheckpt().Epoch)
		if len(peers) >= required {
			break
		}
		log.WithFields(logrus.Fields{
			"suitable": len(peers),
			"required": required,
		}).Info("Waiting for enough suitable peers before syncing")
		time.Sleep(handshakePollingInterval)
	}
}

// waitForStateInitialization makes sure that beacon node is ready to be accessed: it is either
// already properly configured or system waits up until state initialized event is triggered.
func (s *Service) waitForStateInitialization() {
	// Wait for state to be initialized.
	stateChannel := make(chan *feed.Event, 1)
	stateSub := s.stateNotifier.StateFeed().Subscribe(stateChannel)
	defer stateSub.Unsubscribe()
	log.Info("Waiting for state to be initialized")
	for {
		select {
		case event := <-stateChannel:
			if event.Type == statefeed.Initialized {
				data, ok := event.Data.(*statefeed.InitializedData)
				if !ok {
					log.Error("Event feed data is not type *statefeed.InitializedData")
					continue
				}
				log.WithField("starttime", data.StartTime).Debug("Received state initialized event")
				s.genesisChan <- data.StartTime
				return
			}
		case <-s.ctx.Done():
			log.Debug("Context closed, exiting goroutine")
			// Send a zero time in the event we are exiting.
			s.genesisChan <- time.Time{}
			return
		case err := <-stateSub.Err():
			log.WithError(err).Error("Subscription to state notifier failed")
			// Send a zero time in the event we are exiting.
			s.genesisChan <- time.Time{}
			return
		}
	}
}

// markSynced marks node as synced and notifies feed listeners.
func (s *Service) markSynced(genesis time.Time) {
	s.synced.Set()
	s.stateNotifier.StateFeed().Send(&feed.Event{
		Type: statefeed.Synced,
		Data: &statefeed.SyncedData{
			StartTime: genesis,
		},
	})
}

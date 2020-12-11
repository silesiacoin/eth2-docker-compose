package client

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dgraph-io/ristretto"
	ptypes "github.com/gogo/protobuf/types"
	middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_retry "github.com/grpc-ecosystem/go-grpc-middleware/retry"
	grpc_opentracing "github.com/grpc-ecosystem/go-grpc-middleware/tracing/opentracing"
	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	lru "github.com/hashicorp/golang-lru"
	"github.com/pkg/errors"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	pbrpc "github.com/prysmaticlabs/prysm/proto/beacon/rpc/v1"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
	"github.com/prysmaticlabs/prysm/shared/event"
	"github.com/prysmaticlabs/prysm/shared/grpcutils"
	"github.com/prysmaticlabs/prysm/shared/params"
	"github.com/prysmaticlabs/prysm/validator/accounts/wallet"
	"github.com/prysmaticlabs/prysm/validator/db"
	"github.com/prysmaticlabs/prysm/validator/keymanager"
	"github.com/prysmaticlabs/prysm/validator/keymanager/imported"
	slashingprotection "github.com/prysmaticlabs/prysm/validator/slashing-protection"
	"github.com/sirupsen/logrus"
	"go.opencensus.io/plugin/ocgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

var log = logrus.WithField("prefix", "validator")

// SyncChecker is able to determine if a beacon node is currently
// going through chain synchronization.
type SyncChecker interface {
	Syncing(ctx context.Context) (bool, error)
}

// GenesisFetcher can retrieve genesis information such as
// the genesis time and the validator deposit contract address.
type GenesisFetcher interface {
	GenesisInfo(ctx context.Context) (*ethpb.Genesis, error)
}

// BeaconNodeInfoFetcher can retrieve information such as the logs endpoint
// from a beacon node via RPC.
type BeaconNodeInfoFetcher interface {
	BeaconLogsEndpoint(ctx context.Context) (string, error)
}

// ValidatorService represents a service to manage the validator client
// routine.
type ValidatorService struct {
	useWeb                bool
	emitAccountMetrics    bool
	logValidatorBalances  bool
	conn                  *grpc.ClientConn
	grpcRetryDelay        time.Duration
	grpcRetries           uint
	maxCallRecvMsgSize    int
	walletInitializedFeed *event.Feed
	cancel                context.CancelFunc
	db                    db.Database
	dataDir               string
	withCert              string
	endpoint              string
	validator             Validator
	protector             slashingprotection.Protector
	ctx                   context.Context
	keyManager            keymanager.IKeymanager
	grpcHeaders           []string
	graffiti              []byte
}

// Config for the validator service.
type Config struct {
	UseWeb                     bool
	LogValidatorBalances       bool
	EmitAccountMetrics         bool
	WalletInitializedFeed      *event.Feed
	GrpcRetriesFlag            uint
	GrpcRetryDelay             time.Duration
	GrpcMaxCallRecvMsgSizeFlag int
	Protector                  slashingprotection.Protector
	Endpoint                   string
	Validator                  Validator
	ValDB                      db.Database
	KeyManager                 keymanager.IKeymanager
	GraffitiFlag               string
	CertFlag                   string
	DataDir                    string
	GrpcHeadersFlag            string
}

// NewValidatorService creates a new validator service for the service
// registry.
func NewValidatorService(ctx context.Context, cfg *Config) (*ValidatorService, error) {
	ctx, cancel := context.WithCancel(ctx)
	return &ValidatorService{
		ctx:                   ctx,
		cancel:                cancel,
		endpoint:              cfg.Endpoint,
		withCert:              cfg.CertFlag,
		dataDir:               cfg.DataDir,
		graffiti:              []byte(cfg.GraffitiFlag),
		keyManager:            cfg.KeyManager,
		logValidatorBalances:  cfg.LogValidatorBalances,
		emitAccountMetrics:    cfg.EmitAccountMetrics,
		maxCallRecvMsgSize:    cfg.GrpcMaxCallRecvMsgSizeFlag,
		grpcRetries:           cfg.GrpcRetriesFlag,
		grpcRetryDelay:        cfg.GrpcRetryDelay,
		grpcHeaders:           strings.Split(cfg.GrpcHeadersFlag, ","),
		protector:             cfg.Protector,
		validator:             cfg.Validator,
		db:                    cfg.ValDB,
		walletInitializedFeed: cfg.WalletInitializedFeed,
		useWeb:                cfg.UseWeb,
	}, nil
}

// Start the validator service. Launches the main go routine for the validator
// client.
func (v *ValidatorService) Start() {
	streamInterceptor := grpc.WithStreamInterceptor(middleware.ChainStreamClient(
		grpc_opentracing.StreamClientInterceptor(),
		grpc_prometheus.StreamClientInterceptor,
		grpc_retry.StreamClientInterceptor(),
	))
	dialOpts := ConstructDialOptions(
		v.maxCallRecvMsgSize,
		v.withCert,
		v.grpcRetries,
		v.grpcRetryDelay,
		streamInterceptor,
	)
	if dialOpts == nil {
		return
	}

	for _, hdr := range v.grpcHeaders {
		if hdr != "" {
			ss := strings.Split(hdr, "=")
			if len(ss) != 2 {
				log.Warnf("Incorrect gRPC header flag format. Skipping %v", hdr)
				continue
			}
			v.ctx = metadata.AppendToOutgoingContext(v.ctx, ss[0], ss[1])
		}
	}

	conn, err := grpc.DialContext(v.ctx, v.endpoint, dialOpts...)
	if err != nil {
		log.Errorf("Could not dial endpoint: %s, %v", v.endpoint, err)
		return
	}
	if v.withCert != "" {
		log.Info("Established secure gRPC connection")
	}

	v.conn = conn
	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1920, // number of keys to track.
		MaxCost:     192,  // maximum cost of cache, 1 item = 1 cost.
		BufferItems: 64,   // number of keys per Get buffer.
	})
	if err != nil {
		panic(err)
	}

	aggregatedSlotCommitteeIDCache, err := lru.New(int(params.BeaconConfig().MaxCommitteesPerSlot))
	if err != nil {
		log.Errorf("Could not initialize cache: %v", err)
		return
	}

	v.validator = &validator{
		db:                             v.db,
		validatorClient:                ethpb.NewBeaconNodeValidatorClient(v.conn),
		beaconClient:                   ethpb.NewBeaconChainClient(v.conn),
		node:                           ethpb.NewNodeClient(v.conn),
		keyManager:                     v.keyManager,
		graffiti:                       v.graffiti,
		logValidatorBalances:           v.logValidatorBalances,
		emitAccountMetrics:             v.emitAccountMetrics,
		startBalances:                  make(map[[48]byte]uint64),
		prevBalance:                    make(map[[48]byte]uint64),
		attLogs:                        make(map[[32]byte]*attSubmitted),
		domainDataCache:                cache,
		aggregatedSlotCommitteeIDCache: aggregatedSlotCommitteeIDCache,
		protector:                      v.protector,
		voteStats:                      voteStats{startEpoch: ^uint64(0)},
		useWeb:                         v.useWeb,
		walletInitializedFeed:          v.walletInitializedFeed,
	}
	go run(v.ctx, v.validator)
	go v.recheckKeys(v.ctx)
}

// Stop the validator service.
func (v *ValidatorService) Stop() error {
	v.cancel()
	log.Info("Stopping service")
	if v.conn != nil {
		return v.conn.Close()
	}
	return nil
}

// Status of the validator service.
func (v *ValidatorService) Status() error {
	if v.conn == nil {
		return errors.New("no connection to beacon RPC")
	}
	return nil
}

func (v *ValidatorService) recheckKeys(ctx context.Context) {
	var validatingKeys [][48]byte
	var err error
	if v.useWeb {
		initializedChan := make(chan *wallet.Wallet)
		sub := v.walletInitializedFeed.Subscribe(initializedChan)
		cleanup := sub.Unsubscribe
		defer cleanup()
		w := <-initializedChan
		keyManager, err := w.InitializeKeymanager(ctx)
		if err != nil {
			// log.Fatalf will prevent defer from being called
			cleanup()
			log.Fatalf("Could not read keymanager for wallet: %v", err)
		}
		v.keyManager = keyManager
	}
	validatingKeys, err = v.keyManager.FetchValidatingPublicKeys(ctx)
	if err != nil {
		log.WithError(err).Debug("Could not fetch validating keys")
	}
	if err := v.db.UpdatePublicKeysBuckets(validatingKeys); err != nil {
		log.WithError(err).Debug("Could not update public keys buckets")
	}
	go recheckValidatingKeysBucket(ctx, v.db, v.keyManager)
	for _, key := range validatingKeys {
		log.WithField(
			"publicKey", fmt.Sprintf("%#x", bytesutil.Trunc(key[:])),
		).Info("Validating for public key")
	}
}

// ConstructDialOptions constructs a list of grpc dial options
func ConstructDialOptions(
	maxCallRecvMsgSize int,
	withCert string,
	grpcRetries uint,
	grpcRetryDelay time.Duration,
	extraOpts ...grpc.DialOption,
) []grpc.DialOption {
	var transportSecurity grpc.DialOption
	if withCert != "" {
		creds, err := credentials.NewClientTLSFromFile(withCert, "")
		if err != nil {
			log.Errorf("Could not get valid credentials: %v", err)
			return nil
		}
		transportSecurity = grpc.WithTransportCredentials(creds)
	} else {
		transportSecurity = grpc.WithInsecure()
		log.Warn("You are using an insecure gRPC connection. If you are running your beacon node and " +
			"validator on the same machines, you can ignore this message. If you want to know " +
			"how to enable secure connections, see: https://docs.prylabs.network/docs/prysm-usage/secure-grpc")
	}

	if maxCallRecvMsgSize == 0 {
		maxCallRecvMsgSize = 10 * 5 << 20 // Default 50Mb
	}

	dialOpts := []grpc.DialOption{
		transportSecurity,
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(maxCallRecvMsgSize),
			grpc_retry.WithMax(grpcRetries),
			grpc_retry.WithBackoff(grpc_retry.BackoffLinear(grpcRetryDelay)),
		),
		grpc.WithStatsHandler(&ocgrpc.ClientHandler{}),
		grpc.WithUnaryInterceptor(middleware.ChainUnaryClient(
			grpc_opentracing.UnaryClientInterceptor(),
			grpc_prometheus.UnaryClientInterceptor,
			grpc_retry.UnaryClientInterceptor(),
			grpcutils.LogGRPCRequests,
		)),
		grpc.WithChainStreamInterceptor(
			grpcutils.LogGRPCStream,
			grpc_opentracing.StreamClientInterceptor(),
			grpc_prometheus.StreamClientInterceptor,
			grpc_retry.StreamClientInterceptor(),
		),
		grpc.WithResolvers(&multipleEndpointsGrpcResolverBuilder{}),
	}

	dialOpts = append(dialOpts, extraOpts...)
	return dialOpts
}

// Syncing returns whether or not the beacon node is currently synchronizing the chain.
func (v *ValidatorService) Syncing(ctx context.Context) (bool, error) {
	nc := ethpb.NewNodeClient(v.conn)
	resp, err := nc.GetSyncStatus(ctx, &ptypes.Empty{})
	if err != nil {
		return false, err
	}
	return resp.Syncing, nil
}

// GenesisInfo queries the beacon node for the chain genesis info containing
// the genesis time along with the validator deposit contract address.
func (v *ValidatorService) GenesisInfo(ctx context.Context) (*ethpb.Genesis, error) {
	nc := ethpb.NewNodeClient(v.conn)
	return nc.GetGenesis(ctx, &ptypes.Empty{})
}

// BeaconLogsEndpoint retrieves the websocket endpoint string at which
// clients can subscribe to for beacon node logs.
func (v *ValidatorService) BeaconLogsEndpoint(ctx context.Context) (string, error) {
	hc := pbrpc.NewHealthClient(v.conn)
	resp, err := hc.GetLogsEndpoint(ctx, &ptypes.Empty{})
	if err != nil {
		return "", err
	}
	return resp.BeaconLogsEndpoint, nil
}

// to accounts changes in the keymanager, then updates those keys'
// buckets in bolt DB if a bucket for a key does not exist.
func recheckValidatingKeysBucket(ctx context.Context, valDB db.Database, km keymanager.IKeymanager) {
	importedKeymanager, ok := km.(*imported.Keymanager)
	if !ok {
		return
	}
	validatingPubKeysChan := make(chan [][48]byte, 1)
	sub := importedKeymanager.SubscribeAccountChanges(validatingPubKeysChan)
	defer sub.Unsubscribe()
	for {
		select {
		case keys := <-validatingPubKeysChan:
			if err := valDB.UpdatePublicKeysBuckets(keys); err != nil {
				log.WithError(err).Debug("Could not update public keys buckets")
				continue
			}
		case <-ctx.Done():
			return
		}
	}
}

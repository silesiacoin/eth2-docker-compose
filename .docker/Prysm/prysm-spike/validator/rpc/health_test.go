package rpc

import (
	"context"
	"testing"
	"time"

	ptypes "github.com/gogo/protobuf/types"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	pb "github.com/prysmaticlabs/prysm/proto/validator/accounts/v2"
	"github.com/prysmaticlabs/prysm/shared/testutil/require"
	"github.com/prysmaticlabs/prysm/validator/client"
)

type mockSyncChecker struct {
	syncing bool
}

func (m *mockSyncChecker) Syncing(_ context.Context) (bool, error) {
	return m.syncing, nil
}

type mockGenesisFetcher struct{}

func (m *mockGenesisFetcher) GenesisInfo(_ context.Context) (*ethpb.Genesis, error) {
	genesis, err := ptypes.TimestampProto(time.Unix(0, 0))
	if err != nil {
		log.Info(err)
		return nil, err
	}
	return &ethpb.Genesis{
		GenesisTime: genesis,
	}, nil
}

type mockBeaconInfoFetcher struct {
	endpoint string
}

func (m *mockBeaconInfoFetcher) BeaconLogsEndpoint(_ context.Context) (string, error) {
	return m.endpoint, nil
}

func TestServer_GetBeaconNodeConnection(t *testing.T) {
	ctx := context.Background()
	endpoint := "localhost:90210"
	vs, err := client.NewValidatorService(ctx, &client.Config{})
	require.NoError(t, err)
	s := &Server{
		walletInitialized:   true,
		validatorService:    vs,
		syncChecker:         &mockSyncChecker{syncing: false},
		genesisFetcher:      &mockGenesisFetcher{},
		nodeGatewayEndpoint: endpoint,
	}
	got, err := s.GetBeaconNodeConnection(ctx, &ptypes.Empty{})
	require.NoError(t, err)
	want := &pb.NodeConnectionResponse{
		BeaconNodeEndpoint: endpoint,
		Connected:          false,
		Syncing:            false,
		GenesisTime:        uint64(time.Unix(0, 0).Unix()),
	}
	require.DeepEqual(t, want, got)
}

func TestServer_GetLogsEndpoints(t *testing.T) {
	ctx := context.Background()
	s := &Server{
		validatorMonitoringHost: "localhost",
		validatorMonitoringPort: 8081,
		beaconNodeInfoFetcher:   &mockBeaconInfoFetcher{endpoint: "localhost:8080"},
	}
	got, err := s.GetLogsEndpoints(ctx, &ptypes.Empty{})
	require.NoError(t, err)
	want := &pb.LogsEndpointResponse{
		BeaconLogsEndpoint:    "localhost:8080/logs",
		ValidatorLogsEndpoint: "localhost:8081/logs",
	}
	require.DeepEqual(t, want, got)
}

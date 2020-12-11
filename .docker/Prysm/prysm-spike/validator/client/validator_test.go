package client

import (
	"context"
	"errors"
	"io/ioutil"
	"sync"
	"testing"
	"time"

	ptypes "github.com/gogo/protobuf/types"
	"github.com/golang/mock/gomock"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	validatorpb "github.com/prysmaticlabs/prysm/proto/validator/accounts/v2"
	"github.com/prysmaticlabs/prysm/shared/bls"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
	"github.com/prysmaticlabs/prysm/shared/mock"
	"github.com/prysmaticlabs/prysm/shared/params"
	"github.com/prysmaticlabs/prysm/shared/testutil/assert"
	"github.com/prysmaticlabs/prysm/shared/testutil/require"
	"github.com/prysmaticlabs/prysm/validator/db/kv"
	dbTest "github.com/prysmaticlabs/prysm/validator/db/testing"
	"github.com/sirupsen/logrus"
	logTest "github.com/sirupsen/logrus/hooks/test"
)

func init() {
	logrus.SetLevel(logrus.DebugLevel)
	logrus.SetOutput(ioutil.Discard)
}

var _ Validator = (*validator)(nil)

const cancelledCtx = "context has been canceled"

func genMockKeymanger(numKeys int) *mockKeymanager {
	km := make(map[[48]byte]bls.SecretKey, numKeys)
	for i := 0; i < numKeys; i++ {
		k, err := bls.RandKey()
		if err != nil {
			panic(err)
		}
		km[bytesutil.ToBytes48(k.PublicKey().Marshal())] = k
	}

	return &mockKeymanager{keysMap: km}
}

type mockKeymanager struct {
	lock    sync.RWMutex
	keysMap map[[48]byte]bls.SecretKey
}

func (m *mockKeymanager) FetchValidatingPublicKeys(ctx context.Context) ([][48]byte, error) {
	m.lock.RLock()
	defer m.lock.RUnlock()
	keys := make([][48]byte, 0)
	for pubKey := range m.keysMap {
		keys = append(keys, pubKey)
	}
	return keys, nil
}

func (m *mockKeymanager) FetchAllValidatingPublicKeys(ctx context.Context) ([][48]byte, error) {
	m.lock.RLock()
	defer m.lock.RUnlock()
	keys := make([][48]byte, 0)
	for pubKey := range m.keysMap {
		keys = append(keys, pubKey)
	}
	return keys, nil
}

func (m *mockKeymanager) Sign(ctx context.Context, req *validatorpb.SignRequest) (bls.Signature, error) {
	pubKey := [48]byte{}
	copy(pubKey[:], req.PublicKey)
	privKey, ok := m.keysMap[pubKey]
	if !ok {
		return nil, errors.New("not found")
	}
	sig := privKey.Sign(req.SigningRoot)
	return sig, nil
}

func generateMockStatusResponse(pubkeys [][]byte) *ethpb.ValidatorActivationResponse {
	multipleStatus := make([]*ethpb.ValidatorActivationResponse_Status, len(pubkeys))
	for i, key := range pubkeys {
		multipleStatus[i] = &ethpb.ValidatorActivationResponse_Status{
			PublicKey: key,
			Status: &ethpb.ValidatorStatusResponse{
				Status: ethpb.ValidatorStatus_UNKNOWN_STATUS,
			},
		}
	}
	return &ethpb.ValidatorActivationResponse{Statuses: multipleStatus}
}

func TestWaitForChainStart_SetsGenesisInfo(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mock.NewMockBeaconNodeValidatorClient(ctrl)

	db := dbTest.SetupDB(t, [][48]byte{})
	v := validator{
		validatorClient: client,
		db:              db,
	}

	// Make sure its clean at the start.
	savedGenValRoot, err := db.GenesisValidatorsRoot(context.Background())
	require.NoError(t, err)
	assert.DeepEqual(t, []byte(nil), savedGenValRoot, "Unexpected saved genesis validator root")

	genesis := uint64(time.Unix(1, 0).Unix())
	genesisValidatorsRoot := bytesutil.ToBytes32([]byte("validators"))
	clientStream := mock.NewMockBeaconNodeValidator_WaitForChainStartClient(ctrl)
	client.EXPECT().WaitForChainStart(
		gomock.Any(),
		&ptypes.Empty{},
	).Return(clientStream, nil)
	clientStream.EXPECT().Recv().Return(
		&ethpb.ChainStartResponse{
			Started:               true,
			GenesisTime:           genesis,
			GenesisValidatorsRoot: genesisValidatorsRoot[:],
		},
		nil,
	)
	require.NoError(t, v.WaitForChainStart(context.Background()))
	savedGenValRoot, err = db.GenesisValidatorsRoot(context.Background())
	require.NoError(t, err)

	assert.DeepEqual(t, genesisValidatorsRoot[:], savedGenValRoot, "Unexpected saved genesis validator root")
	assert.Equal(t, genesis, v.genesisTime, "Unexpected chain start time")
	assert.NotNil(t, v.ticker, "Expected ticker to be set, received nil")

	// Make sure theres no errors running if its the same data.
	client.EXPECT().WaitForChainStart(
		gomock.Any(),
		&ptypes.Empty{},
	).Return(clientStream, nil)
	clientStream.EXPECT().Recv().Return(
		&ethpb.ChainStartResponse{
			Started:               true,
			GenesisTime:           genesis,
			GenesisValidatorsRoot: genesisValidatorsRoot[:],
		},
		nil,
	)
	require.NoError(t, v.WaitForChainStart(context.Background()))
}

func TestWaitForChainStart_SetsGenesisInfo_IncorrectSecondTry(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mock.NewMockBeaconNodeValidatorClient(ctrl)

	db := dbTest.SetupDB(t, [][48]byte{})
	v := validator{
		validatorClient: client,
		db:              db,
	}
	genesis := uint64(time.Unix(1, 0).Unix())
	genesisValidatorsRoot := bytesutil.ToBytes32([]byte("validators"))
	clientStream := mock.NewMockBeaconNodeValidator_WaitForChainStartClient(ctrl)
	client.EXPECT().WaitForChainStart(
		gomock.Any(),
		&ptypes.Empty{},
	).Return(clientStream, nil)
	clientStream.EXPECT().Recv().Return(
		&ethpb.ChainStartResponse{
			Started:               true,
			GenesisTime:           genesis,
			GenesisValidatorsRoot: genesisValidatorsRoot[:],
		},
		nil,
	)
	require.NoError(t, v.WaitForChainStart(context.Background()))
	savedGenValRoot, err := db.GenesisValidatorsRoot(context.Background())
	require.NoError(t, err)

	assert.DeepEqual(t, genesisValidatorsRoot[:], savedGenValRoot, "Unexpected saved genesis validator root")
	assert.Equal(t, genesis, v.genesisTime, "Unexpected chain start time")
	assert.NotNil(t, v.ticker, "Expected ticker to be set, received nil")

	genesisValidatorsRoot = bytesutil.ToBytes32([]byte("badvalidators"))

	// Make sure theres no errors running if its the same data.
	client.EXPECT().WaitForChainStart(
		gomock.Any(),
		&ptypes.Empty{},
	).Return(clientStream, nil)
	clientStream.EXPECT().Recv().Return(
		&ethpb.ChainStartResponse{
			Started:               true,
			GenesisTime:           genesis,
			GenesisValidatorsRoot: genesisValidatorsRoot[:],
		},
		nil,
	)
	err = v.WaitForChainStart(context.Background())
	require.ErrorContains(t, "does not match root saved", err)
}

func TestWaitForChainStart_ContextCanceled(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mock.NewMockBeaconNodeValidatorClient(ctrl)

	v := validator{
		//keyManager:      testKeyManager,
		validatorClient: client,
	}
	genesis := uint64(time.Unix(0, 0).Unix())
	genesisValidatorsRoot := bytesutil.PadTo([]byte("validators"), 32)
	clientStream := mock.NewMockBeaconNodeValidator_WaitForChainStartClient(ctrl)
	client.EXPECT().WaitForChainStart(
		gomock.Any(),
		&ptypes.Empty{},
	).Return(clientStream, nil)
	clientStream.EXPECT().Recv().Return(
		&ethpb.ChainStartResponse{
			Started:               true,
			GenesisTime:           genesis,
			GenesisValidatorsRoot: genesisValidatorsRoot,
		},
		nil,
	)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.ErrorContains(t, cancelledCtx, v.WaitForChainStart(ctx))
}

func TestWaitForChainStart_StreamSetupFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mock.NewMockBeaconNodeValidatorClient(ctrl)

	privKey, err := bls.RandKey()
	require.NoError(t, err)
	pubKey := [48]byte{}
	copy(pubKey[:], privKey.PublicKey().Marshal())
	km := &mockKeymanager{
		keysMap: make(map[[48]byte]bls.SecretKey),
	}
	v := validator{
		validatorClient: client,
		keyManager:      km,
	}
	clientStream := mock.NewMockBeaconNodeValidator_WaitForChainStartClient(ctrl)
	client.EXPECT().WaitForChainStart(
		gomock.Any(),
		&ptypes.Empty{},
	).Return(clientStream, errors.New("failed stream"))
	err = v.WaitForChainStart(context.Background())
	want := "could not setup beacon chain ChainStart streaming client"
	assert.ErrorContains(t, want, err)
}

func TestWaitForChainStart_ReceiveErrorFromStream(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mock.NewMockBeaconNodeValidatorClient(ctrl)

	v := validator{
		//keyManager:      testKeyManager,
		validatorClient: client,
	}
	clientStream := mock.NewMockBeaconNodeValidator_WaitForChainStartClient(ctrl)
	client.EXPECT().WaitForChainStart(
		gomock.Any(),
		&ptypes.Empty{},
	).Return(clientStream, nil)
	clientStream.EXPECT().Recv().Return(
		nil,
		errors.New("fails"),
	)
	err := v.WaitForChainStart(context.Background())
	want := "could not receive ChainStart from stream"
	assert.ErrorContains(t, want, err)
}

func TestWaitActivation_ContextCanceled(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mock.NewMockBeaconNodeValidatorClient(ctrl)
	privKey, err := bls.RandKey()
	require.NoError(t, err)
	pubKey := [48]byte{}
	copy(pubKey[:], privKey.PublicKey().Marshal())
	km := &mockKeymanager{
		keysMap: map[[48]byte]bls.SecretKey{
			pubKey: privKey,
		},
	}
	v := validator{
		validatorClient: client,
		keyManager:      km,
	}
	clientStream := mock.NewMockBeaconNodeValidator_WaitForActivationClient(ctrl)

	client.EXPECT().WaitForActivation(
		gomock.Any(),
		&ethpb.ValidatorActivationRequest{
			PublicKeys: [][]byte{pubKey[:]},
		},
	).Return(clientStream, nil)
	clientStream.EXPECT().Recv().Return(
		&ethpb.ValidatorActivationResponse{},
		nil,
	)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.ErrorContains(t, cancelledCtx, v.WaitForActivation(ctx))
}

func TestWaitActivation_StreamSetupFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mock.NewMockBeaconNodeValidatorClient(ctrl)
	privKey, err := bls.RandKey()
	require.NoError(t, err)
	pubKey := [48]byte{}
	copy(pubKey[:], privKey.PublicKey().Marshal())
	km := &mockKeymanager{
		keysMap: map[[48]byte]bls.SecretKey{
			pubKey: privKey,
		},
	}
	v := validator{
		validatorClient: client,
		keyManager:      km,
	}
	clientStream := mock.NewMockBeaconNodeValidator_WaitForActivationClient(ctrl)
	client.EXPECT().WaitForActivation(
		gomock.Any(),
		&ethpb.ValidatorActivationRequest{
			PublicKeys: [][]byte{pubKey[:]},
		},
	).Return(clientStream, errors.New("failed stream"))
	err = v.WaitForActivation(context.Background())
	want := "could not setup validator WaitForActivation streaming client"
	assert.ErrorContains(t, want, err)
}

func TestWaitActivation_ReceiveErrorFromStream(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mock.NewMockBeaconNodeValidatorClient(ctrl)

	privKey, err := bls.RandKey()
	require.NoError(t, err)
	pubKey := [48]byte{}
	copy(pubKey[:], privKey.PublicKey().Marshal())
	km := &mockKeymanager{
		keysMap: map[[48]byte]bls.SecretKey{
			pubKey: privKey,
		},
	}
	v := validator{
		validatorClient: client,
		keyManager:      km,
	}
	clientStream := mock.NewMockBeaconNodeValidator_WaitForActivationClient(ctrl)
	client.EXPECT().WaitForActivation(
		gomock.Any(),
		&ethpb.ValidatorActivationRequest{
			PublicKeys: [][]byte{pubKey[:]},
		},
	).Return(clientStream, nil)
	clientStream.EXPECT().Recv().Return(
		nil,
		errors.New("fails"),
	)
	err = v.WaitForActivation(context.Background())
	want := "could not receive validator activation from stream"
	assert.ErrorContains(t, want, err)
}

func TestWaitActivation_LogsActivationEpochOK(t *testing.T) {
	hook := logTest.NewGlobal()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mock.NewMockBeaconNodeValidatorClient(ctrl)
	privKey, err := bls.RandKey()
	require.NoError(t, err)
	pubKey := [48]byte{}
	copy(pubKey[:], privKey.PublicKey().Marshal())
	km := &mockKeymanager{
		keysMap: map[[48]byte]bls.SecretKey{
			pubKey: privKey,
		},
	}
	v := validator{
		validatorClient: client,
		keyManager:      km,
		genesisTime:     1,
	}
	resp := generateMockStatusResponse([][]byte{pubKey[:]})
	resp.Statuses[0].Status.Status = ethpb.ValidatorStatus_ACTIVE
	clientStream := mock.NewMockBeaconNodeValidator_WaitForActivationClient(ctrl)
	client.EXPECT().WaitForActivation(
		gomock.Any(),
		&ethpb.ValidatorActivationRequest{
			PublicKeys: [][]byte{pubKey[:]},
		},
	).Return(clientStream, nil)
	clientStream.EXPECT().Recv().Return(
		resp,
		nil,
	)
	assert.NoError(t, v.WaitForActivation(context.Background()), "Could not wait for activation")
	require.LogsContain(t, hook, "Validator activated")
}

func TestWaitForActivation_Exiting(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mock.NewMockBeaconNodeValidatorClient(ctrl)
	privKey, err := bls.RandKey()
	require.NoError(t, err)
	pubKey := [48]byte{}
	copy(pubKey[:], privKey.PublicKey().Marshal())
	km := &mockKeymanager{
		keysMap: map[[48]byte]bls.SecretKey{
			pubKey: privKey,
		},
	}
	v := validator{
		validatorClient: client,
		keyManager:      km,
		genesisTime:     1,
	}
	resp := generateMockStatusResponse([][]byte{pubKey[:]})
	resp.Statuses[0].Status.Status = ethpb.ValidatorStatus_EXITING
	clientStream := mock.NewMockBeaconNodeValidator_WaitForActivationClient(ctrl)
	client.EXPECT().WaitForActivation(
		gomock.Any(),
		&ethpb.ValidatorActivationRequest{
			PublicKeys: [][]byte{pubKey[:]},
		},
	).Return(clientStream, nil)
	clientStream.EXPECT().Recv().Return(
		resp,
		nil,
	)
	require.NoError(t, v.WaitForActivation(context.Background()))
}

func TestCanonicalHeadSlot_FailedRPC(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mock.NewMockBeaconChainClient(ctrl)
	v := validator{
		beaconClient: client,
		genesisTime:  1,
	}
	client.EXPECT().GetChainHead(
		gomock.Any(),
		gomock.Any(),
	).Return(nil, errors.New("failed"))
	_, err := v.CanonicalHeadSlot(context.Background())
	assert.ErrorContains(t, "failed", err)
}

func TestCanonicalHeadSlot_OK(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mock.NewMockBeaconChainClient(ctrl)
	v := validator{
		beaconClient: client,
	}
	client.EXPECT().GetChainHead(
		gomock.Any(),
		gomock.Any(),
	).Return(&ethpb.ChainHead{HeadSlot: 0}, nil)
	headSlot, err := v.CanonicalHeadSlot(context.Background())
	require.NoError(t, err)
	assert.Equal(t, uint64(0), headSlot, "Mismatch slots")
}

func TestWaitMultipleActivation_LogsActivationEpochOK(t *testing.T) {
	hook := logTest.NewGlobal()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mock.NewMockBeaconNodeValidatorClient(ctrl)
	privKey, err := bls.RandKey()
	require.NoError(t, err)
	pubKey := [48]byte{}
	copy(pubKey[:], privKey.PublicKey().Marshal())
	km := &mockKeymanager{
		keysMap: map[[48]byte]bls.SecretKey{
			pubKey: privKey,
		},
	}
	v := validator{
		validatorClient: client,
		keyManager:      km,
		genesisTime:     1,
	}

	resp := generateMockStatusResponse([][]byte{pubKey[:]})
	resp.Statuses[0].Status.Status = ethpb.ValidatorStatus_ACTIVE
	clientStream := mock.NewMockBeaconNodeValidator_WaitForActivationClient(ctrl)
	client.EXPECT().WaitForActivation(
		gomock.Any(),
		&ethpb.ValidatorActivationRequest{
			PublicKeys: [][]byte{pubKey[:]},
		},
	).Return(clientStream, nil)
	clientStream.EXPECT().Recv().Return(
		resp,
		nil,
	)
	require.NoError(t, v.WaitForActivation(context.Background()), "Could not wait for activation")
	require.LogsContain(t, hook, "Validator activated")
}

func TestWaitActivation_NotAllValidatorsActivatedOK(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mock.NewMockBeaconNodeValidatorClient(ctrl)

	privKey, err := bls.RandKey()
	require.NoError(t, err)
	pubKey := [48]byte{}
	copy(pubKey[:], privKey.PublicKey().Marshal())
	km := &mockKeymanager{
		keysMap: map[[48]byte]bls.SecretKey{
			pubKey: privKey,
		},
	}
	v := validator{
		validatorClient: client,
		keyManager:      km,
		genesisTime:     1,
	}
	resp := generateMockStatusResponse([][]byte{pubKey[:]})
	resp.Statuses[0].Status.Status = ethpb.ValidatorStatus_ACTIVE
	clientStream := mock.NewMockBeaconNodeValidator_WaitForActivationClient(ctrl)
	client.EXPECT().WaitForActivation(
		gomock.Any(),
		gomock.Any(),
	).Return(clientStream, nil)
	clientStream.EXPECT().Recv().Return(
		&ethpb.ValidatorActivationResponse{},
		nil,
	)
	clientStream.EXPECT().Recv().Return(
		resp,
		nil,
	)
	assert.NoError(t, v.WaitForActivation(context.Background()), "Could not wait for activation")
}

func TestWaitSync_ContextCanceled(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	n := mock.NewMockNodeClient(ctrl)

	v := validator{
		node: n,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	n.EXPECT().GetSyncStatus(
		gomock.Any(),
		gomock.Any(),
	).Return(&ethpb.SyncStatus{Syncing: true}, nil)

	assert.ErrorContains(t, cancelledCtx, v.WaitForSync(ctx))
}

func TestWaitSync_NotSyncing(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	n := mock.NewMockNodeClient(ctrl)

	v := validator{
		node: n,
	}

	n.EXPECT().GetSyncStatus(
		gomock.Any(),
		gomock.Any(),
	).Return(&ethpb.SyncStatus{Syncing: false}, nil)

	require.NoError(t, v.WaitForSync(context.Background()))
}

func TestWaitSync_Syncing(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	n := mock.NewMockNodeClient(ctrl)

	v := validator{
		node: n,
	}

	n.EXPECT().GetSyncStatus(
		gomock.Any(),
		gomock.Any(),
	).Return(&ethpb.SyncStatus{Syncing: true}, nil)

	n.EXPECT().GetSyncStatus(
		gomock.Any(),
		gomock.Any(),
	).Return(&ethpb.SyncStatus{Syncing: false}, nil)

	require.NoError(t, v.WaitForSync(context.Background()))
}

func TestUpdateDuties_DoesNothingWhenNotEpochStart_AlreadyExistingAssignments(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mock.NewMockBeaconNodeValidatorClient(ctrl)

	slot := uint64(1)
	v := validator{
		validatorClient: client,
		duties: &ethpb.DutiesResponse{
			Duties: []*ethpb.DutiesResponse_Duty{
				{
					Committee:      []uint64{},
					AttesterSlot:   10,
					CommitteeIndex: 20,
				},
			},
		},
	}
	client.EXPECT().GetDuties(
		gomock.Any(),
		gomock.Any(),
	).Times(0)

	assert.NoError(t, v.UpdateDuties(context.Background(), slot), "Could not update assignments")
}

func TestUpdateDuties_ReturnsError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mock.NewMockBeaconNodeValidatorClient(ctrl)

	privKey, err := bls.RandKey()
	require.NoError(t, err)
	pubKey := [48]byte{}
	copy(pubKey[:], privKey.PublicKey().Marshal())
	km := &mockKeymanager{
		keysMap: map[[48]byte]bls.SecretKey{
			pubKey: privKey,
		},
	}
	v := validator{
		validatorClient: client,
		keyManager:      km,
		duties: &ethpb.DutiesResponse{
			Duties: []*ethpb.DutiesResponse_Duty{
				{
					CommitteeIndex: 1,
				},
			},
		},
	}

	expected := errors.New("bad")

	client.EXPECT().GetDuties(
		gomock.Any(),
		gomock.Any(),
	).Return(nil, expected)

	assert.ErrorContains(t, expected.Error(), v.UpdateDuties(context.Background(), params.BeaconConfig().SlotsPerEpoch))
	assert.Equal(t, (*ethpb.DutiesResponse)(nil), v.duties, "Assignments should have been cleared on failure")
}

func TestUpdateDuties_OK(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mock.NewMockBeaconNodeValidatorClient(ctrl)

	slot := params.BeaconConfig().SlotsPerEpoch
	privKey, err := bls.RandKey()
	require.NoError(t, err)
	pubKey := [48]byte{}
	copy(pubKey[:], privKey.PublicKey().Marshal())
	km := &mockKeymanager{
		keysMap: map[[48]byte]bls.SecretKey{
			pubKey: privKey,
		},
	}
	resp := &ethpb.DutiesResponse{
		Duties: []*ethpb.DutiesResponse_Duty{
			{
				AttesterSlot:   params.BeaconConfig().SlotsPerEpoch,
				ValidatorIndex: 200,
				CommitteeIndex: 100,
				Committee:      []uint64{0, 1, 2, 3},
				PublicKey:      []byte("testPubKey_1"),
				ProposerSlots:  []uint64{params.BeaconConfig().SlotsPerEpoch + 1},
			},
		},
	}
	v := validator{
		keyManager:      km,
		validatorClient: client,
	}
	client.EXPECT().GetDuties(
		gomock.Any(),
		gomock.Any(),
	).Return(resp, nil)

	client.EXPECT().GetDuties(
		gomock.Any(),
		gomock.Any(),
	).Return(resp, nil)

	client.EXPECT().SubscribeCommitteeSubnets(
		gomock.Any(),
		gomock.Any(),
	).Return(nil, nil)

	require.NoError(t, v.UpdateDuties(context.Background(), slot), "Could not update assignments")
	assert.Equal(t, params.BeaconConfig().SlotsPerEpoch+1, v.duties.Duties[0].ProposerSlots[0], "Unexpected validator assignments")
	assert.Equal(t, params.BeaconConfig().SlotsPerEpoch, v.duties.Duties[0].AttesterSlot, "Unexpected validator assignments")
	assert.Equal(t, resp.Duties[0].CommitteeIndex, v.duties.Duties[0].CommitteeIndex, "Unexpected validator assignments")
	assert.Equal(t, resp.Duties[0].ValidatorIndex, v.duties.Duties[0].ValidatorIndex, "Unexpected validator assignments")
}

func TestUpdateProtections_OK(t *testing.T) {
	ctx := context.Background()
	pubKey1 := [48]byte{1}
	pubKey2 := [48]byte{2}
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mock.NewMockBeaconNodeValidatorClient(ctrl)
	db := dbTest.SetupDB(t, [][48]byte{pubKey1, pubKey2})
	history := kv.NewAttestationHistoryArray(2)
	history, err := history.SetTargetData(ctx, 1, &kv.HistoryData{Source: 0, SigningRoot: []byte{1}})
	require.NoError(t, err)
	history, err = history.SetTargetData(ctx, 2, &kv.HistoryData{Source: 1, SigningRoot: []byte{2}})
	require.NoError(t, err)
	history, err = history.SetLatestEpochWritten(ctx, 2)
	require.NoError(t, err)
	history2 := kv.NewAttestationHistoryArray(3)
	history2, err = history2.SetTargetData(ctx, 3, &kv.HistoryData{Source: 2, SigningRoot: []byte{1}})
	require.NoError(t, err)
	history2, err = history2.SetLatestEpochWritten(ctx, 3)
	require.NoError(t, err)

	histories := make(map[[48]byte]kv.EncHistoryData)
	histories[pubKey1] = history
	histories[pubKey2] = history2
	require.NoError(t, db.SaveAttestationHistoryForPubKeysV2(context.Background(), histories))

	slot := params.BeaconConfig().SlotsPerEpoch
	duties := &ethpb.DutiesResponse{
		CurrentEpochDuties: []*ethpb.DutiesResponse_Duty{
			{
				AttesterSlot:   slot,
				ValidatorIndex: 200,
				CommitteeIndex: 100,
				Committee:      []uint64{0, 1, 2, 3},
				PublicKey:      pubKey1[:],
			},
			{
				AttesterSlot:   slot,
				ValidatorIndex: 201,
				CommitteeIndex: 100,
				Committee:      []uint64{0, 1, 2, 3},
				PublicKey:      pubKey2[:],
			},
		},
	}
	v := validator{
		db:              db,
		validatorClient: client,
		duties:          duties,
	}

	require.NoError(t, v.UpdateProtections(context.Background(), slot), "Could not update assignments")
	require.DeepEqual(t, history, v.attesterHistoryByPubKey[pubKey1], "Unexpected retrieved history")
	require.DeepEqual(t, history2, v.attesterHistoryByPubKey[pubKey2], "Unexpected retrieved history")
}

func TestSaveProtection_OK(t *testing.T) {
	pubKey1 := [48]byte{1}
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mock.NewMockBeaconNodeValidatorClient(ctrl)
	db := dbTest.SetupDB(t, [][48]byte{pubKey1})
	ctx := context.Background()

	cleanHistories, err := db.AttestationHistoryForPubKeysV2(context.Background(), [][48]byte{pubKey1})
	require.NoError(t, err)
	v := validator{
		db:                      db,
		validatorClient:         client,
		attesterHistoryByPubKey: cleanHistories,
	}

	history1 := cleanHistories[pubKey1]
	sr := [32]byte{1}
	newHist, err := kv.MarkAllAsAttestedSinceLatestWrittenEpoch(ctx, history1, 1, &kv.HistoryData{
		Source:      0,
		SigningRoot: sr[:],
	})
	require.NoError(t, err)
	history1 = newHist

	cleanHistories[pubKey1] = history1

	v.attesterHistoryByPubKey = cleanHistories
	require.NoError(t, v.SaveProtection(context.Background(), pubKey1), "Could not update assignments")
	savedHistories, err := db.AttestationHistoryForPubKeysV2(context.Background(), [][48]byte{pubKey1})
	require.NoError(t, err)

	require.DeepEqual(t, history1, savedHistories[pubKey1], "Unexpected retrieved history")
}

func TestRolesAt_OK(t *testing.T) {
	v, m, validatorKey, finish := setup(t)
	defer finish()

	v.duties = &ethpb.DutiesResponse{
		Duties: []*ethpb.DutiesResponse_Duty{
			{
				CommitteeIndex: 1,
				AttesterSlot:   1,
				PublicKey:      validatorKey.PublicKey().Marshal(),
			},
		},
	}

	m.validatorClient.EXPECT().DomainData(
		gomock.Any(), // ctx
		gomock.Any(), // epoch
	).Return(&ethpb.DomainResponse{SignatureDomain: make([]byte, 32)}, nil /*err*/)

	roleMap, err := v.RolesAt(context.Background(), 1)
	require.NoError(t, err)

	assert.Equal(t, ValidatorRole(roleAttester), roleMap[bytesutil.ToBytes48(validatorKey.PublicKey().Marshal())][0])
}

func TestRolesAt_DoesNotAssignProposer_Slot0(t *testing.T) {
	v, m, validatorKey, finish := setup(t)
	defer finish()

	v.duties = &ethpb.DutiesResponse{
		Duties: []*ethpb.DutiesResponse_Duty{
			{
				CommitteeIndex: 1,
				AttesterSlot:   0,
				ProposerSlots:  []uint64{0},
				PublicKey:      validatorKey.PublicKey().Marshal(),
			},
		},
	}

	m.validatorClient.EXPECT().DomainData(
		gomock.Any(), // ctx
		gomock.Any(), // epoch
	).Return(&ethpb.DomainResponse{SignatureDomain: make([]byte, 32)}, nil /*err*/)

	roleMap, err := v.RolesAt(context.Background(), 0)
	require.NoError(t, err)

	assert.Equal(t, ValidatorRole(roleAttester), roleMap[bytesutil.ToBytes48(validatorKey.PublicKey().Marshal())][0])
}

func TestCheckAndLogValidatorStatus_OK(t *testing.T) {
	nonexistentIndex := ^uint64(0)
	type statusTest struct {
		name   string
		status *ethpb.ValidatorActivationResponse_Status
		log    string
		active bool
	}
	pubKeys := [][]byte{
		bytesutil.Uint64ToBytesLittleEndian(0),
		bytesutil.Uint64ToBytesLittleEndian(1),
		bytesutil.Uint64ToBytesLittleEndian(2),
		bytesutil.Uint64ToBytesLittleEndian(3),
	}
	tests := []statusTest{
		{
			name: "UNKNOWN_STATUS, no deposit found yet",
			status: &ethpb.ValidatorActivationResponse_Status{
				PublicKey: pubKeys[0],
				Index:     nonexistentIndex,
				Status: &ethpb.ValidatorStatusResponse{
					Status: ethpb.ValidatorStatus_UNKNOWN_STATUS,
				},
			},
			log: "Waiting for deposit to be observed by beacon node",
		},
		{
			name: "DEPOSITED, deposit found",
			status: &ethpb.ValidatorActivationResponse_Status{
				PublicKey: pubKeys[0],
				Index:     nonexistentIndex,
				Status: &ethpb.ValidatorStatusResponse{
					Status:                 ethpb.ValidatorStatus_DEPOSITED,
					DepositInclusionSlot:   50,
					Eth1DepositBlockNumber: 400,
				},
			},
			log: "Deposit for validator received but not processed into the beacon state\" eth1DepositBlockNumber=400 expectedInclusionSlot=50",
		},
		{
			name: "DEPOSITED into state",
			status: &ethpb.ValidatorActivationResponse_Status{
				PublicKey: pubKeys[0],
				Index:     30,
				Status: &ethpb.ValidatorStatusResponse{
					Status:                    ethpb.ValidatorStatus_DEPOSITED,
					PositionInActivationQueue: 30,
				},
			},
			log: "Deposit processed, entering activation queue after finalization\" index=30 positionInActivationQueue=30",
		},
		{
			name: "PENDING",
			status: &ethpb.ValidatorActivationResponse_Status{
				PublicKey: pubKeys[0],
				Index:     50,
				Status: &ethpb.ValidatorStatusResponse{
					Status:                    ethpb.ValidatorStatus_PENDING,
					ActivationEpoch:           params.BeaconConfig().FarFutureEpoch,
					PositionInActivationQueue: 6,
				},
			},
			log: "Waiting to be assigned activation epoch\" index=50 positionInActivationQueue=6",
		},
		{
			name: "PENDING",
			status: &ethpb.ValidatorActivationResponse_Status{
				PublicKey: pubKeys[0],
				Index:     89,
				Status: &ethpb.ValidatorStatusResponse{
					Status:                    ethpb.ValidatorStatus_PENDING,
					ActivationEpoch:           60,
					PositionInActivationQueue: 5,
				},
			},
			log: "Waiting for activation\" activationEpoch=60 index=89",
		},
		{
			name: "EXITED",
			status: &ethpb.ValidatorActivationResponse_Status{
				PublicKey: pubKeys[0],
				Status: &ethpb.ValidatorStatusResponse{
					Status: ethpb.ValidatorStatus_EXITED,
				},
			},
			log: "Validator exited",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			hook := logTest.NewGlobal()
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			client := mock.NewMockBeaconNodeValidatorClient(ctrl)
			v := validator{
				validatorClient: client,
				duties: &ethpb.DutiesResponse{
					Duties: []*ethpb.DutiesResponse_Duty{
						{
							CommitteeIndex: 1,
						},
					},
				},
			}

			active := v.checkAndLogValidatorStatus([]*ethpb.ValidatorActivationResponse_Status{test.status})
			require.Equal(t, test.active, active, "Expected key to be active")
			require.LogsContain(t, hook, test.log)
		})
	}
}

func TestAllValidatorsAreExited_AllExited(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mock.NewMockBeaconNodeValidatorClient(ctrl)

	statuses := []*ethpb.ValidatorStatusResponse{
		{Status: ethpb.ValidatorStatus_EXITED},
		{Status: ethpb.ValidatorStatus_EXITED},
	}

	client.EXPECT().MultipleValidatorStatus(
		gomock.Any(), // ctx
		gomock.Any(), // request
	).Return(&ethpb.MultipleValidatorStatusResponse{Statuses: statuses}, nil /*err*/)

	v := validator{keyManager: genMockKeymanger(2), validatorClient: client}
	exited, err := v.AllValidatorsAreExited(context.Background())
	require.NoError(t, err)
	assert.Equal(t, true, exited)
}

func TestAllValidatorsAreExited_NotAllExited(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mock.NewMockBeaconNodeValidatorClient(ctrl)

	statuses := []*ethpb.ValidatorStatusResponse{
		{Status: ethpb.ValidatorStatus_ACTIVE},
		{Status: ethpb.ValidatorStatus_EXITED},
	}

	client.EXPECT().MultipleValidatorStatus(
		gomock.Any(), // ctx
		gomock.Any(), // request
	).Return(&ethpb.MultipleValidatorStatusResponse{Statuses: statuses}, nil /*err*/)

	v := validator{keyManager: genMockKeymanger(2), validatorClient: client}
	exited, err := v.AllValidatorsAreExited(context.Background())
	require.NoError(t, err)
	assert.Equal(t, false, exited)
}

func TestAllValidatorsAreExited_PartialResult(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mock.NewMockBeaconNodeValidatorClient(ctrl)

	statuses := []*ethpb.ValidatorStatusResponse{
		{Status: ethpb.ValidatorStatus_EXITED},
	}

	client.EXPECT().MultipleValidatorStatus(
		gomock.Any(), // ctx
		gomock.Any(), // request
	).Return(&ethpb.MultipleValidatorStatusResponse{Statuses: statuses}, nil /*err*/)

	v := validator{keyManager: genMockKeymanger(2), validatorClient: client}
	exited, err := v.AllValidatorsAreExited(context.Background())
	require.ErrorContains(t, "number of status responses did not match number of requested keys", err)
	assert.Equal(t, false, exited)
}

func TestAllValidatorsAreExited_NoKeys(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mock.NewMockBeaconNodeValidatorClient(ctrl)
	v := validator{keyManager: genMockKeymanger(0), validatorClient: client}
	exited, err := v.AllValidatorsAreExited(context.Background())
	require.NoError(t, err)
	assert.Equal(t, false, exited)
}

// TestAllValidatorsAreExited_CorrectRequest is a regression test that checks if the request contains the correct keys
func TestAllValidatorsAreExited_CorrectRequest(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mock.NewMockBeaconNodeValidatorClient(ctrl)

	// Create two different public keys
	pubKey0 := [48]byte{1, 2, 3, 4}
	pubKey1 := [48]byte{6, 7, 8, 9}
	// This is the request expected from AllValidatorsAreExited()
	request := &ethpb.MultipleValidatorStatusRequest{
		PublicKeys: [][]byte{
			pubKey0[:],
			pubKey1[:],
		},
	}
	statuses := []*ethpb.ValidatorStatusResponse{
		{Status: ethpb.ValidatorStatus_ACTIVE},
		{Status: ethpb.ValidatorStatus_EXITED},
	}

	client.EXPECT().MultipleValidatorStatus(
		gomock.Any(), // ctx
		request,      // request
	).Return(&ethpb.MultipleValidatorStatusResponse{Statuses: statuses}, nil /*err*/)

	keysMap := make(map[[48]byte]bls.SecretKey)
	// secretKey below is just filler and is used multiple times
	secretKeyBytes := [32]byte{1}
	secretKey, err := bls.SecretKeyFromBytes(secretKeyBytes[:])
	require.NoError(t, err)
	keysMap[pubKey0] = secretKey
	keysMap[pubKey1] = secretKey

	// If AllValidatorsAreExited does not create the expected request, this test will fail
	v := validator{keyManager: &mockKeymanager{keysMap: keysMap}, validatorClient: client}
	exited, err := v.AllValidatorsAreExited(context.Background())
	require.NoError(t, err)
	assert.Equal(t, false, exited)
}

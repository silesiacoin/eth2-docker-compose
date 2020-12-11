package spectest

import (
	"encoding/hex"
	"errors"
	"path"
	"testing"

	"github.com/ghodss/yaml"
	"github.com/prysmaticlabs/prysm/shared/bls"
	"github.com/prysmaticlabs/prysm/shared/bls/common"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
	"github.com/prysmaticlabs/prysm/shared/featureconfig"
	"github.com/prysmaticlabs/prysm/shared/testutil"
	"github.com/prysmaticlabs/prysm/shared/testutil/require"
)

func TestAggregateVerifyYaml(t *testing.T) {
	flags := &featureconfig.Flags{}
	reset := featureconfig.InitWithReset(flags)
	t.Run("herumi", testAggregateVerifyYaml)
	reset()

	flags.EnableBlst = true
	reset = featureconfig.InitWithReset(flags)
	t.Run("blst", testAggregateVerifyYaml)
	reset()
}

func testAggregateVerifyYaml(t *testing.T) {
	testFolders, testFolderPath := testutil.TestFolders(t, "general", "bls/aggregate_verify/small")

	for i, folder := range testFolders {
		t.Run(folder.Name(), func(t *testing.T) {
			file, err := testutil.BazelFileBytes(path.Join(testFolderPath, folder.Name(), "data.yaml"))
			require.NoError(t, err)
			test := &AggregateVerifyTest{}
			require.NoError(t, yaml.Unmarshal(file, test))
			pubkeys := make([]common.PublicKey, 0, len(test.Input.Pubkeys))
			msgs := make([][32]byte, 0, len(test.Input.Messages))
			for _, pubKey := range test.Input.Pubkeys {
				pkBytes, err := hex.DecodeString(pubKey[2:])
				require.NoError(t, err)
				pk, err := bls.PublicKeyFromBytes(pkBytes)
				if err != nil {
					if test.Output == false && errors.Is(err, common.ErrInfinitePubKey) {
						return
					}
					t.Fatalf("cannot unmarshal pubkey: %v", err)
				}
				pubkeys = append(pubkeys, pk)
			}
			for _, msg := range test.Input.Messages {
				msgBytes, err := hex.DecodeString(msg[2:])
				require.NoError(t, err)
				require.Equal(t, 32, len(msgBytes))
				msgs = append(msgs, bytesutil.ToBytes32(msgBytes))
			}
			sigBytes, err := hex.DecodeString(test.Input.Signature[2:])
			require.NoError(t, err)
			sig, err := bls.SignatureFromBytes(sigBytes)
			if err != nil {
				if test.Output == false {
					return
				}
				t.Fatalf("Cannot unmarshal input to signature: %v", err)
			}

			verified := sig.AggregateVerify(pubkeys, msgs)
			if verified != test.Output {
				t.Fatalf("Signature does not match the expected verification output. "+
					"Expected %#v but received %#v for test case %d", test.Output, verified, i)
			}
		})
	}
}

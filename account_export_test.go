package weshnet

import (
	"archive/tar"
	"context"
	"io"
	"io/ioutil"
	"os"
	"testing"

	"github.com/ipfs/go-cid"
	ds "github.com/ipfs/go-datastore"
	dsync "github.com/ipfs/go-datastore/sync"
	"github.com/libp2p/go-libp2p/core/crypto"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/require"

	orbitdb "berty.tech/go-orbit-db"
	"berty.tech/go-orbit-db/pubsub/pubsubraw"
	"berty.tech/weshnet/internal/datastoreutil"
	"berty.tech/weshnet/pkg/cryptoutil"
	"berty.tech/weshnet/pkg/ipfsutil"
	"berty.tech/weshnet/pkg/protocoltypes"
	"berty.tech/weshnet/pkg/testutil"
	"berty.tech/weshnet/pkg/tinder"
)

func Test_service_exportAccountKey(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mn := mocknet.New()
	defer mn.Close()

	msrv := tinder.NewMockDriverServer()

	dsA := dsync.MutexWrap(ds.NewMapDatastore())
	nodeA, closeNodeA := NewTestingProtocol(ctx, t, &TestingOpts{
		Mocknet:         mn,
		DiscoveryServer: msrv,
	}, dsA)
	defer closeNodeA()

	// time.Sleep(time.Second * 5)

	s, ok := nodeA.Service.(*service)
	require.True(t, ok)

	tmpFile, err := ioutil.TempFile(os.TempDir(), "test-export-")
	require.NoError(t, err)

	defer os.Remove(tmpFile.Name())

	tw := tar.NewWriter(tmpFile)

	err = s.exportAccountKey(tw)
	require.NoError(t, err)

	err = tw.Close()
	require.NoError(t, err)

	_, err = tmpFile.Seek(0, io.SeekStart)
	require.NoError(t, err)

	tr := tar.NewReader(tmpFile)
	header, err := tr.Next()
	require.NoError(t, err)
	require.Equal(t, exportAccountKeyFilename, header.Name)

	keyContents := make([]byte, header.Size)

	size, err := tr.Read(keyContents)
	require.Equal(t, int(header.Size), size)

	sk, err := crypto.UnmarshalPrivateKey(keyContents)
	require.NoError(t, err)

	accountSK, err := s.deviceKeystore.AccountPrivKey()
	require.NoError(t, err)

	require.True(t, accountSK.Equals(sk))
}

func Test_service_exportAccountProofKey(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mn := mocknet.New()
	defer mn.Close()

	msrv := tinder.NewMockDriverServer()

	dsA := dsync.MutexWrap(ds.NewMapDatastore())
	nodeA, closeNodeA := NewTestingProtocol(ctx, t, &TestingOpts{
		Mocknet:         mn,
		DiscoveryServer: msrv,
	}, dsA)
	defer closeNodeA()

	s, ok := nodeA.Service.(*service)
	require.True(t, ok)

	tmpFile, err := ioutil.TempFile(os.TempDir(), "test-export-")
	require.NoError(t, err)

	defer os.Remove(tmpFile.Name())

	tw := tar.NewWriter(tmpFile)
	err = s.exportAccountProofKey(tw)
	require.NoError(t, err)

	err = tw.Close()
	require.NoError(t, err)

	_, err = tmpFile.Seek(0, io.SeekStart)
	require.NoError(t, err)

	tr := tar.NewReader(tmpFile)
	header, err := tr.Next()
	require.NoError(t, err)
	require.Equal(t, exportAccountProofKeyFilename, header.Name)

	keyContents := make([]byte, header.Size)

	size, err := tr.Read(keyContents)
	require.Equal(t, int(header.Size), size)

	sk, err := crypto.UnmarshalPrivateKey(keyContents)
	require.NoError(t, err)

	accountProofSK, err := s.deviceKeystore.AccountProofPrivKey()
	require.NoError(t, err)

	require.True(t, accountProofSK.Equals(sk))
}

func TestRestoreAccount(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger, cleanup := testutil.Logger(t)
	defer cleanup()

	mn := mocknet.New()
	defer mn.Close()

	msrv := tinder.NewMockDriverServer()

	tmpFile, err := ioutil.TempFile(os.TempDir(), "test-export-")
	require.NoError(t, err)

	expectedMessages := map[cid.Cid][]byte{}
	var nodeAInstanceConfig *protocoltypes.ServiceGetConfiguration_Reply

	g, _, err := NewGroupMultiMember()
	require.NoError(t, err)

	defer os.Remove(tmpFile.Name())

	{
		dsA := dsync.MutexWrap(ds.NewMapDatastore())
		nodeA, closeNodeA := NewTestingProtocol(ctx, t, &TestingOpts{
			Mocknet: mn,
		}, dsA)

		serviceA, ok := nodeA.Service.(*service)
		require.True(t, ok)

		nodeAInstanceConfig, err = nodeA.Client.ServiceGetConfiguration(ctx, &protocoltypes.ServiceGetConfiguration_Request{})
		require.NoError(t, err)
		require.NotNil(t, nodeAInstanceConfig)

		testPayload1 := []byte("testMessage1")
		testPayload2 := []byte("testMessage2")
		testPayload3 := []byte("testMessage3")
		testPayload4 := []byte("testMessage4")

		accountGroup := serviceA.getAccountGroup()
		require.NotNil(t, accountGroup)

		op, err := accountGroup.messageStore.AddMessage(ctx, testPayload1)
		require.NoError(t, err)

		expectedMessages[op.GetEntry().GetHash()] = testPayload1

		op, err = accountGroup.messageStore.AddMessage(ctx, testPayload2)
		require.NoError(t, err)

		expectedMessages[op.GetEntry().GetHash()] = testPayload2

		_, err = nodeA.Client.MultiMemberGroupJoin(ctx, &protocoltypes.MultiMemberGroupJoin_Request{Group: g})
		require.NoError(t, err)

		_, err = nodeA.Client.ActivateGroup(ctx, &protocoltypes.ActivateGroup_Request{GroupPK: g.PublicKey})
		require.NoError(t, err)

		op, err = serviceA.openedGroups[string(g.PublicKey)].messageStore.AddMessage(ctx, testPayload3)
		require.NoError(t, err)

		expectedMessages[op.GetEntry().GetHash()] = testPayload3

		op, err = serviceA.openedGroups[string(g.PublicKey)].messageStore.AddMessage(ctx, testPayload4)
		require.NoError(t, err)

		expectedMessages[op.GetEntry().GetHash()] = testPayload4

		require.NoError(t, serviceA.export(ctx, tmpFile))

		closeNodeA()
		require.NoError(t, dsA.Close())
	}

	_, err = tmpFile.Seek(0, io.SeekStart)
	require.NoError(t, err)

	{
		dsB := dsync.MutexWrap(ds.NewMapDatastore())
		ipfsNodeB := ipfsutil.TestingCoreAPIUsingMockNet(ctx, t, &ipfsutil.TestingAPIOpts{
			Mocknet:   mn,
			Datastore: dsB,
		})

		dksB := cryptoutil.NewDeviceKeystore(ipfsutil.NewDatastoreKeystore(datastoreutil.NewNamespacedDatastore(dsB, ds.NewKey(NamespaceDeviceKeystore))), nil)

		odb, err := NewWeshOrbitDB(ctx, ipfsNodeB.API(), &NewOrbitDBOptions{
			NewOrbitDBOptions: orbitdb.NewOrbitDBOptions{
				PubSub: pubsubraw.NewPubSub(ipfsNodeB.PubSub(), ipfsNodeB.MockNode().PeerHost.ID(), logger, nil),
				Logger: logger,
			},
			Datastore:      dsB,
			DeviceKeystore: dksB,
		})
		require.NoError(t, err)

		err = RestoreAccountExport(ctx, tmpFile, ipfsNodeB.API(), odb, logger)
		require.NoError(t, err)

		nodeB, closeNodeB := NewTestingProtocol(ctx, t, &TestingOpts{
			Mocknet:         mn,
			DiscoveryServer: msrv,
			DeviceKeystore:  dksB,
			CoreAPIMock:     ipfsNodeB,
			OrbitDB:         odb,
		}, dsB)
		defer closeNodeB()

		nodeBInstanceConfig, err := nodeB.Client.ServiceGetConfiguration(ctx, &protocoltypes.ServiceGetConfiguration_Request{})
		require.NoError(t, err)

		require.NotNil(t, nodeBInstanceConfig)
		require.Equal(t, nodeAInstanceConfig.AccountPK, nodeBInstanceConfig.AccountPK)
		require.NotEqual(t, nodeAInstanceConfig.DevicePK, nodeBInstanceConfig.DevicePK)
		require.Equal(t, nodeAInstanceConfig.AccountGroupPK, nodeBInstanceConfig.AccountGroupPK)

		accountGroup := nodeB.Service.(*service).getAccountGroup()
		require.NotNil(t, accountGroup)

		entries := accountGroup.messageStore.OpLog().GetEntries()
		for _, evt := range entries.Slice() {
			_, ok := expectedMessages[evt.GetHash()]
			require.True(t, ok)
		}

		_, err = nodeB.Service.ActivateGroup(ctx, &protocoltypes.ActivateGroup_Request{GroupPK: g.PublicKey})
		require.NoError(t, err)

		for _, gPK := range [][]byte{nodeBInstanceConfig.AccountGroupPK, g.PublicKey} {
			sub, err := nodeB.Client.GroupMessageList(
				ctx,
				&protocoltypes.GroupMessageList_Request{
					GroupPK:  gPK,
					UntilNow: true,
				},
			)
			require.NoError(t, err)

			for {
				evt, err := sub.Recv()
				if err != nil {
					require.Equal(t, io.EOF, err)
					break
				}

				id, err := cid.Parse(evt.EventContext.ID)
				require.NoError(t, err)

				ref, ok := expectedMessages[id]
				require.True(t, ok)
				require.Equal(t, ref, evt.Message)

				delete(expectedMessages, id)
			}
		}

		require.Empty(t, expectedMessages)
	}
	// TODO: test account metadata entries
}

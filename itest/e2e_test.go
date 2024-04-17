//go:build e2e
// +build e2e

package e2etest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	staking "github.com/babylonchain/babylon/btcstaking"

	"github.com/babylonchain/covenant-signer/internal/btcclient"
	"github.com/babylonchain/covenant-signer/internal/config"
	"github.com/babylonchain/covenant-signer/internal/logger"
	"github.com/babylonchain/covenant-signer/internal/services/signer"
	"github.com/babylonchain/covenant-signer/itest/containers"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"github.com/stretchr/testify/require"
)

var (
	netParams              = &chaincfg.RegressionNetParams
	eventuallyPollInterval = 100 * time.Millisecond
	eventuallyTimeout      = 10 * time.Second
)

type TestManager struct {
	t                   *testing.T
	bitcoindHandler     *BitcoindTestHandler
	walletPass          string
	btcClient           *btcclient.BtcClient
	covenantKeys        []*btcec.PrivateKey
	covenantQuorum      uint32
	finalityProviderKey *btcec.PrivateKey
	stakerAddress       btcutil.Address
	stakerPrivKey       *btcec.PrivateKey
	stakerPubKey        *btcec.PublicKey
	magicBytes          []byte
	pipeLineConfig      *config.Config
}

type stakingData struct {
	stakingAmount  btcutil.Amount
	stakingTime    uint16
	stakingFeeRate btcutil.Amount
	unbondingTime  uint16
	unbondingFee   btcutil.Amount
}

func defaultStakingData() *stakingData {
	return &stakingData{
		stakingAmount:  btcutil.Amount(100000),
		stakingTime:    10000,
		stakingFeeRate: btcutil.Amount(5000), //feeRatePerKb
		unbondingTime:  100,
		unbondingFee:   btcutil.Amount(10000),
	}
}

func (d *stakingData) unbondingAmount() btcutil.Amount {
	return d.stakingAmount - d.unbondingFee
}

func StartManager(
	t *testing.T,
	numMatureOutputsInWallet uint32) *TestManager {
	// logger := logger.DefaultLogger()
	m, err := containers.NewManager()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = m.ClearResources()
	})

	h := NewBitcoindHandler(t, m)
	h.Start()

	// Give some time to launch and bitcoind
	time.Sleep(2 * time.Second)

	passphrase := "pass"
	_ = h.CreateWallet("test-wallet", passphrase)
	// only outputs which are 100 deep are mature
	_ = h.GenerateBlocks(int(numMatureOutputsInWallet) + 100)

	numCovenantKeys := 3
	quorum := uint32(2)
	var coventantKeys []*btcec.PrivateKey
	for i := 0; i < numCovenantKeys; i++ {
		key, err := btcec.NewPrivateKey()
		require.NoError(t, err)
		coventantKeys = append(coventantKeys, key)
	}

	var covenantKeysStrings []string
	for _, key := range coventantKeys {
		covenantKeysStrings = append(covenantKeysStrings, hex.EncodeToString(key.Serialize()))
	}

	appConfig := config.DefaultConfig()

	appConfig.Btc.Host = "127.0.0.1:18443"
	appConfig.Btc.User = "user"
	appConfig.Btc.Pass = "pass"
	appConfig.Btc.Network = netParams.Name

	// Client for testing purposes
	client, err := btcclient.NewBtcClient(&appConfig.Btc)
	require.NoError(t, err)

	outputs, err := client.ListOutputs(true)
	require.NoError(t, err)
	require.Len(t, outputs, int(numMatureOutputsInWallet))

	// easiest way to get address controlled by wallet is to retrive address from one
	// of the outputs
	output := outputs[0]
	walletAddress, err := btcutil.DecodeAddress(output.Address, netParams)
	require.NoError(t, err)

	err = client.UnlockWallet(20, passphrase)
	require.NoError(t, err)
	stakerPrivKey, err := client.DumpPrivateKey(walletAddress)
	require.NoError(t, err)

	fpKey, err := btcec.NewPrivateKey()
	require.NoError(t, err)

	return &TestManager{
		t:                   t,
		bitcoindHandler:     h,
		walletPass:          passphrase,
		btcClient:           client,
		covenantKeys:        coventantKeys,
		covenantQuorum:      quorum,
		finalityProviderKey: fpKey,
		stakerAddress:       walletAddress,
		stakerPrivKey:       stakerPrivKey,
		stakerPubKey:        stakerPrivKey.PubKey(),
		magicBytes:          []byte{0x0, 0x1, 0x2, 0x3},
		pipeLineConfig:      appConfig,
	}
}

func (tm *TestManager) covenantPubKeys() []*btcec.PublicKey {
	var pubKeys []*btcec.PublicKey
	for _, key := range tm.covenantKeys {
		k := key
		pubKeys = append(pubKeys, k.PubKey())
	}
	return pubKeys
}

type stakingTxSigInfo struct {
	stakingTxHash *chainhash.Hash
	stakingOutput *wire.TxOut
}

func (tm *TestManager) sendStakingTxToBtc(d *stakingData) *stakingTxSigInfo {
	info, err := staking.BuildV0IdentifiableStakingOutputs(
		tm.magicBytes,
		tm.stakerPubKey,
		tm.finalityProviderKey.PubKey(),
		tm.covenantPubKeys(),
		tm.covenantQuorum,
		d.stakingTime,
		d.stakingAmount,
		netParams,
	)
	require.NoError(tm.t, err)

	err = tm.btcClient.UnlockWallet(20, tm.walletPass)
	require.NoError(tm.t, err)
	// staking output will always have index 0
	tx, err := tm.btcClient.CreateAndSignTx(
		[]*wire.TxOut{info.StakingOutput, info.OpReturnOutput},
		d.stakingFeeRate,
		tm.stakerAddress,
	)
	require.NoError(tm.t, err)

	hash, err := tm.btcClient.SendTx(tx)
	require.NoError(tm.t, err)
	// generate blocks to make sure tx will be included into chain
	_ = tm.bitcoindHandler.GenerateBlocks(2)
	return &stakingTxSigInfo{
		stakingTxHash: hash,
		stakingOutput: info.StakingOutput,
	}
}

type unbondingTxWithMetadata struct {
	unbondingTx *wire.MsgTx
	signature   *schnorr.Signature
}

func (tm *TestManager) createUnbondingTxAndSignByStaker(
	si *stakingTxSigInfo,
	d *stakingData,
) *unbondingTxWithMetadata {

	info, err := staking.BuildV0IdentifiableStakingOutputs(
		tm.magicBytes,
		tm.stakerPubKey,
		tm.finalityProviderKey.PubKey(),
		tm.covenantPubKeys(),
		tm.covenantQuorum,
		d.stakingTime,
		d.stakingAmount,
		netParams,
	)
	require.NoError(tm.t, err)

	unbondingPathInfo, err := info.UnbondingPathSpendInfo()
	require.NoError(tm.t, err)

	unbondingInfo, err := staking.BuildUnbondingInfo(
		tm.stakerPubKey,
		[]*btcec.PublicKey{tm.finalityProviderKey.PubKey()},
		tm.covenantPubKeys(),
		tm.covenantQuorum,
		d.unbondingTime,
		d.unbondingAmount(),
		netParams,
	)
	require.NoError(tm.t, err)

	unbondingTx := wire.NewMsgTx(2)
	unbondingTx.AddTxIn(wire.NewTxIn(wire.NewOutPoint(si.stakingTxHash, 0), nil, nil))
	unbondingTx.AddTxOut(unbondingInfo.UnbondingOutput)

	unbondingTxSignature, err := staking.SignTxWithOneScriptSpendInputFromScript(
		unbondingTx,
		si.stakingOutput,
		tm.stakerPrivKey,
		unbondingPathInfo.RevealedLeaf.Script,
	)
	require.NoError(tm.t, err)

	return &unbondingTxWithMetadata{
		unbondingTx: unbondingTx,
		signature:   unbondingTxSignature,
	}
}

func (tm *TestManager) createNUnbondingTransactions(n int, d *stakingData) ([]*unbondingTxWithMetadata, []*wire.MsgTx) {
	var infos []*stakingTxSigInfo
	var sendStakingTransactions []*wire.MsgTx

	for i := 0; i < n; i++ {
		sInfo := tm.sendStakingTxToBtc(d)
		conf, status, err := tm.btcClient.TxDetails(sInfo.stakingTxHash, sInfo.stakingOutput.PkScript)
		require.NoError(tm.t, err)
		require.Equal(tm.t, btcclient.TxInChain, status)
		infos = append(infos, sInfo)
		sendStakingTransactions = append(sendStakingTransactions, conf.Tx)
	}

	var unbondingTxs []*unbondingTxWithMetadata
	for _, i := range infos {
		info := i
		ubs := tm.createUnbondingTxAndSignByStaker(
			info,
			d,
		)
		unbondingTxs = append(unbondingTxs, ubs)
	}

	return unbondingTxs, sendStakingTransactions
}

func TestSigner(t *testing.T) {
	logger := logger.DefaultLogger()
	signerServer, err := signer.New(
		context.TODO(),
		logger,
		&signer.Services{},
	)
	require.NoError(t, err)

	go func() {
		_ = signerServer.Start()
	}()

	time.Sleep(3 * time.Second)

	fmt.Println("hello ")

	url := "http://127.0.0.1:9701"

	var tx = wire.NewMsgTx(2)

	hash := sha256.Sum256([]byte{1})
	btcdHash, err := chainhash.NewHash(hash[:])
	require.NoError(t, err)
	fakeOutpoint := wire.NewOutPoint(
		btcdHash,
		1,
	)
	fakeInput := wire.NewTxIn(fakeOutpoint, nil, nil)

	tx.AddTxIn(fakeInput)
	tx.AddTxOut(wire.NewTxOut(1, []byte{1}))

	res, err := signer.RequestCovenantSignaure(
		context.TODO(),
		url,
		10*time.Second,
		tx,
	)

	require.NoError(t, err)
	require.NotNil(t, res)
}

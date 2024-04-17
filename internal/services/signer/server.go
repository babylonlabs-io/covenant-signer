package signer

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/babylonchain/babylon/btcstaking"
	"github.com/babylonchain/covenant-signer/internal/utils"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/go-chi/chi/v5"
)

type SpendPathDescription struct {
	ControlBlock *txscript.ControlBlock
	ScriptLeaf   *txscript.TapLeaf
}

type SigningRequest struct {
	StakingOutput        *wire.TxOut
	UnbondingTransaction *wire.MsgTx
	CovenantPublicKey    *btcec.PublicKey
	SpendDescription     *SpendPathDescription
}

type SigningResult struct {
	Signature *schnorr.Signature
}

type ExternalBtcSigner interface {
	RawSignature(ctx context.Context, request *SigningRequest) (*SigningResult, error)
}

type TxInfo struct {
	Tx                *wire.MsgTx
	TxInclusionHeight uint32
}

type BtcChainInfo interface {
	// Returns only transactions inluded in canonical chain
	// passing pkScript as argument make it light client friendly
	TxByHash(ctx context.Context, txHash *chainhash.Hash, pkScript []byte) (*TxInfo, error)

	BestBlockHeight(ctx context.Context) (uint32, error)
}

type BabylonParams struct {
	CovenantPublicKeys []*btcec.PublicKey
	CovenantQuorum     uint32
	MagicBytes         []byte
	W                  uint32
}

type BabylonParamsRetriever interface {
	Params(ctx context.Context) (*BabylonParams, error)
}

type Services struct {
	s   ExternalBtcSigner
	r   BtcChainInfo
	p   BabylonParamsRetriever
	net *chaincfg.Params
}

func NewServices(
	s ExternalBtcSigner,
	r BtcChainInfo,
	p BabylonParamsRetriever,
	net *chaincfg.Params,
) *Services {
	return &Services{s: s, r: r, p: p, net: net}
}

type handler struct {
	services *Services
}

func (h *handler) SignUnbonding(request *http.Request) (*Result, *Error) {
	payload := &SignUnbondingTxRequest{}
	err := json.NewDecoder(request.Body).Decode(payload)
	if err != nil {
		return nil, NewErrorWithMsg(http.StatusBadRequest, BadRequest, "invalid request payload")
	}

	covenantKeyBytes, err := hex.DecodeString(payload.CovenantPublicKey)

	if err != nil {
		return nil, NewErrorWithMsg(http.StatusBadRequest, BadRequest, "invalid covenant signer public key")
	}

	covnentSignerPubKey, err := btcec.ParsePubKey(covenantKeyBytes)

	if err != nil {
		return nil, NewErrorWithMsg(http.StatusBadRequest, BadRequest, "invalid covenant signer public key")
	}

	unbondingTx, _, err := utils.NewBTCTxFromHex(payload.UnbondingTxHex)

	if err != nil {
		return nil, NewErrorWithMsg(http.StatusBadRequest, BadRequest, "invalid unbonding tx")
	}

	if err := btcstaking.IsSimpleTransfer(unbondingTx); err != nil {
		return nil, NewErrorWithMsg(http.StatusBadRequest, BadRequest, "invalid unbonding tx")
	}

	stakingOutputPkScript, err := hex.DecodeString(payload.StakingOutputPkScriptHex)

	if err != nil {
		return nil, NewErrorWithMsg(http.StatusBadRequest, BadRequest, "invalid staking output pk script")
	}

	script, err := txscript.ParsePkScript(stakingOutputPkScript)

	if err != nil {
		return nil, NewErrorWithMsg(http.StatusBadRequest, BadRequest, "invalid staking output pk script")
	}

	if script.Class() != txscript.WitnessV1TaprootTy {
		return nil, NewErrorWithMsg(http.StatusBadRequest, BadRequest, "invalid staking output pk script")
	}

	stakingTxHash := unbondingTx.TxIn[0].PreviousOutPoint.Hash

	stakingTxInfo, err := h.services.r.TxByHash(request.Context(), &stakingTxHash, stakingOutputPkScript)

	if err != nil {
		return nil, NewErrorWithMsg(http.StatusBadRequest, BadRequest, "failed to get staking tx")
	}

	bestBlock, err := h.services.r.BestBlockHeight(request.Context())

	if err != nil {
		return nil, NewErrorWithMsg(http.StatusInternalServerError, InternalServiceError, "failed to get best block height")
	}

	// TODO: This should probably be done when service is started
	params, err := h.services.p.Params(request.Context())

	if err != nil {
		return nil, NewErrorWithMsg(http.StatusInternalServerError, InternalServiceError, "failed to get babylon system parameters")
	}

	if bestBlock-stakingTxInfo.TxInclusionHeight < params.W {
		return nil, NewErrorWithMsg(http.StatusBadRequest, BadRequest, "staking tx is not deep enoung in btc chains")
	}

	parsedStakingTransaction, err := btcstaking.ParseV0StakingTx(
		stakingTxInfo.Tx,
		params.MagicBytes,
		params.CovenantPublicKeys,
		params.CovenantQuorum,
		h.services.net)

	if err != nil {
		return nil, NewErrorWithMsg(http.StatusBadRequest, BadRequest, "failed to parse staking tx")
	}

	// TODO Add more checs for unbonding tx:
	// - wheter it has valid taproot output
	// - wheter it has correct fee
	// - wheter commits to valid script etc.

	stakingInfo, err := btcstaking.BuildStakingInfo(
		parsedStakingTransaction.OpReturnData.StakerPublicKey.PubKey,
		[]*btcec.PublicKey{parsedStakingTransaction.OpReturnData.FinalityProviderPublicKey.PubKey},
		params.CovenantPublicKeys,
		params.CovenantQuorum,
		parsedStakingTransaction.OpReturnData.StakingTime,
		btcutil.Amount(parsedStakingTransaction.StakingOutput.Value),
		h.services.net,
	)

	if err != nil {
		return nil, NewErrorWithMsg(http.StatusInternalServerError, InternalServiceError, "failed to build staking info")
	}

	unbondingPathInfo, err := stakingInfo.UnbondingPathSpendInfo()

	if err != nil {
		return nil, NewErrorWithMsg(http.StatusInternalServerError, InternalServiceError, "failed to build unbonding path info")
	}

	sig, err := h.services.s.RawSignature(request.Context(), &SigningRequest{
		StakingOutput:        parsedStakingTransaction.StakingOutput,
		UnbondingTransaction: unbondingTx,
		CovenantPublicKey:    covnentSignerPubKey,
		SpendDescription: &SpendPathDescription{
			ControlBlock: &unbondingPathInfo.ControlBlock,
			ScriptLeaf:   &unbondingPathInfo.RevealedLeaf,
		},
	})

	if err != nil {
		return nil, NewErrorWithMsg(http.StatusInternalServerError, InternalServiceError, "failed to sign unbonding tx")
	}

	resp := SignUnbondingTxResponse{
		SignatureHex: hex.EncodeToString(sig.Signature.Serialize()),
	}

	return NewResult(resp), nil
}

func newHandler(
	_ context.Context, services *Services,
) (*handler, error) {
	return &handler{
		services: services,
	}, nil
}

type SigningServer struct {
	logger     *slog.Logger
	httpServer *http.Server
	handler    *handler
}

func (a *SigningServer) SetupRoutes(r *chi.Mux) {
	handler := a.handler
	r.Post("/v1/sign-unbonding-tx", registerHandler(a.logger, handler.SignUnbonding))
}

func New(
	ctx context.Context,
	logger *slog.Logger,
	s *Services,
) (*SigningServer, error) {
	r := chi.NewRouter()

	// TODO: Add middlewares
	// r.Use(middlewares.CorsMiddleware(cfg))
	// r.Use(middlewares.TracingMiddleware)
	// r.Use(middlewares.LoggingMiddleware)
	// TODO: TLS configuration if server is to be exposed over the internet, if it supposed to
	// be behind some reverse proxy like nginx or cloudflare, then it's not needed.
	// Probaby it needs to be configurable

	srv := &http.Server{
		Addr:         fmt.Sprintf("%s:%s", "127.0.0.1", "9701"),
		WriteTimeout: 30 * time.Second,
		ReadTimeout:  30 * time.Second,
		IdleTimeout:  30 * time.Second,
		Handler:      r,
	}

	handler, err := newHandler(ctx, s)
	if err != nil {
		logger.Error("error while setting up handlers")
	}

	server := &SigningServer{
		httpServer: srv,
		handler:    handler,
		logger:     logger,
	}
	server.SetupRoutes(r)
	return server, nil
}

func (a *SigningServer) Start() error {
	return a.httpServer.ListenAndServe()
}

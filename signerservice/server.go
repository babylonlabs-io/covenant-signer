package signerservice

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/babylonchain/covenant-signer/config"
	s "github.com/babylonchain/covenant-signer/signerapp"
	"github.com/babylonchain/covenant-signer/utils"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/go-chi/chi/v5"
)

type handler struct {
	s *s.SignerApp
}

func (h *handler) SignUnbonding(request *http.Request) (*Result, *Error) {
	payload := &SignUnbondingTxRequest{}
	err := json.NewDecoder(request.Body).Decode(payload)
	if err != nil {
		return nil, NewErrorWithMsg(http.StatusBadRequest, BadRequest, "invalid request payload")
	}

	pkScript, err := hex.DecodeString(payload.StakingOutputPkScriptHex)

	if err != nil {
		return nil, NewErrorWithMsg(http.StatusBadRequest, BadRequest, "invalid staking output pk script")
	}

	covenantPublicKeyBytes, err := hex.DecodeString(payload.CovenantPublicKey)

	if err != nil {
		return nil, NewErrorWithMsg(http.StatusBadRequest, BadRequest, "invalid covenant public key")
	}

	covenantPublicKey, err := btcec.ParsePubKey(covenantPublicKeyBytes)

	if err != nil {
		return nil, NewErrorWithMsg(http.StatusBadRequest, BadRequest, "invalid covenant public key")
	}

	unbondingTx, _, err := utils.NewBTCTxFromHex(payload.UnbondingTxHex)

	if err != nil {
		return nil, NewErrorWithMsg(http.StatusBadRequest, BadRequest, "invalid unbonding transaction")
	}

	sig, err := h.s.SignUnbondingTransaction(
		request.Context(),
		pkScript,
		unbondingTx,
		covenantPublicKey,
	)

	if err != nil {
		// TODO Properly translate errors between layers
		return nil, NewErrorWithMsg(http.StatusInternalServerError, InternalServiceError, err.Error())
	}

	resp := SignUnbondingTxResponse{
		SignatureHex: hex.EncodeToString(sig.Serialize()),
	}

	return NewResult(resp), nil
}

func newHandler(
	_ context.Context, s *s.SignerApp,
) (*handler, error) {
	return &handler{
		s: s,
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
	cfg *config.ParsedConfig,
	signer *s.SignerApp,
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
		Addr:         fmt.Sprintf("%s:%d", cfg.ServerConfig.Host, cfg.ServerConfig.Port),
		WriteTimeout: cfg.ServerConfig.WriteTimeout,
		ReadTimeout:  cfg.ServerConfig.ReadTimeout,
		IdleTimeout:  cfg.ServerConfig.IdleTimeout,
		Handler:      r,
	}

	handler, err := newHandler(ctx, signer)
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

func (s *SigningServer) Start() error {
	return s.httpServer.ListenAndServe()
}

func (s *SigningServer) Stop(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

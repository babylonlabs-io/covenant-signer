package signerservice

import (
	"context"
	"fmt"
	"net/http"

	m "github.com/babylonlabs-io/covenant-signer/observability/metrics"
	"github.com/babylonlabs-io/covenant-signer/signerservice/handlers"
	"github.com/babylonlabs-io/covenant-signer/signerservice/middlewares"
	"github.com/rs/zerolog/log"

	"github.com/babylonlabs-io/covenant-signer/config"
	s "github.com/babylonlabs-io/covenant-signer/signerapp"
	"github.com/go-chi/chi/v5"
)

type SigningServer struct {
	httpServer *http.Server
	handler    *handlers.Handler
}

func (a *SigningServer) SetupRoutes(r *chi.Mux) {
	handler := a.handler
	r.Post("/v1/sign-unbonding-tx", registerHandler(handler.SignUnbonding))
}

func New(
	ctx context.Context,
	cfg *config.ParsedConfig,
	signer *s.SignerApp,
	metrics *m.CovenantSignerMetrics,
) (*SigningServer, error) {
	r := chi.NewRouter()

	// TODO: Add middlewares
	// r.Use(middlewares.CorsMiddleware(cfg))
	r.Use(middlewares.TracingMiddleware)
	r.Use(middlewares.LoggingMiddleware)
	r.Use(middlewares.ContentLengthMiddleware(int64(cfg.ServerConfig.MaxContentLength)))
	// TODO: TLS configuration if server is to be exposed over the internet, if it supposed to
	// be behind some reverse proxy like nginx or cloudflare, then it's not needed.
	// Probably it needs to be configurable

	// Add HMAC authentication middleware if configured
	if cfg.ServerConfig.HMACKey != "" {
		log.Info().Msg("HMAC authentication enabled")
		r.Use(middlewares.HMACAuthMiddleware(cfg.ServerConfig.HMACKey))
	} else {
		log.Warn().Msg("HMAC authentication disabled - no key configured")
	}

	srv := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.ServerConfig.Host, cfg.ServerConfig.Port),
		WriteTimeout: cfg.ServerConfig.WriteTimeout,
		ReadTimeout:  cfg.ServerConfig.ReadTimeout,
		IdleTimeout:  cfg.ServerConfig.IdleTimeout,
		Handler:      r,
	}

	h, err := handlers.NewHandler(ctx, signer, metrics)
	if err != nil {
		log.Fatal().Err(err).Msg("error while setting up handlers")
	}

	server := &SigningServer{
		httpServer: srv,
		handler:    h,
	}
	server.SetupRoutes(r)
	return server, nil
}

func (s *SigningServer) Start() error {
	log.Info().Msgf("Starting server on %s", s.httpServer.Addr)
	return s.httpServer.ListenAndServe()
}

func (s *SigningServer) Stop(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

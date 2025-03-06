package middlewares

import (
	"encoding/json"
	"net/http"

	"github.com/babylonlabs-io/covenant-signer/signerservice/types"
	"github.com/babylonlabs-io/covenant-signer/utils"
	"github.com/rs/zerolog/log"
)

// HMACAuthMiddleware creates a middleware that verifies HMAC authentication
func HMACAuthMiddleware(hmacKey string) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip HMAC verification if no key is configured
			if hmacKey == "" {
				next.ServeHTTP(w, r)
				return
			}

			receivedHMAC := r.Header.Get(utils.HeaderCovenantHMAC)
			if receivedHMAC == "" {
				log.Debug().Msg("Request rejected: Missing HMAC header")
				RespondWithError(w, types.NewUnauthorizedError("missing HMAC authentication header"))
				return
			}

			body, newBody, err := utils.RewindRequestBody(r.Body)
			if err != nil {
				log.Error().Err(err).Msg("Failed to read request body for HMAC verification")
				RespondWithError(w, types.NewInternalServiceError(err))
				return
			}

			r.Body = newBody

			valid, err := utils.ValidateHMAC(hmacKey, body, receivedHMAC)
			if err != nil {
				log.Error().Err(err).Msg("Error validating HMAC")
				RespondWithError(w, types.NewInternalServiceError(err))
				return
			}

			if !valid {
				log.Debug().Msg("Request rejected: Invalid HMAC")
				RespondWithError(w, types.NewUnauthorizedError("invalid HMAC authentication"))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func RespondWithError(w http.ResponseWriter, appErr *types.Error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(appErr.StatusCode)

	response := map[string]interface{}{
		"error": map[string]interface{}{
			"code":    appErr.ErrorCode.String(),
			"message": appErr.Error(),
		},
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to generate error response"))
	}
}

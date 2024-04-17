package signerservice

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

type ErrorResponse struct {
	ErrorCode string `json:"errorCode"`
	Message   string `json:"message"`
}

func newInternalServiceError() *ErrorResponse {
	return &ErrorResponse{
		ErrorCode: InternalServiceError.String(),
		Message:   "Internal service error",
	}
}

func (e *ErrorResponse) Error() string {
	return e.Message
}

type Result struct {
	Data   interface{}
	Status int
}

type PublicResponse[T any] struct {
	Data T `json:"data"`
}

func NewResult[T any](data T) *Result {
	res := &PublicResponse[T]{Data: data}
	return &Result{Data: res, Status: http.StatusOK}
}

func registerHandler(logger *slog.Logger, handlerFunc func(*http.Request) (*Result, *Error)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		// Set up metrics recording for the endpoint

		// Handle the actual business logic
		result, err := handlerFunc(r)

		if err != nil {
			if http.StatusText(err.StatusCode) == "" {
				err.StatusCode = http.StatusInternalServerError
			}

			errorResponse := &ErrorResponse{
				ErrorCode: string(err.ErrorCode),
				Message:   err.Err.Error(),
			}
			// Log the error
			if err.StatusCode >= http.StatusInternalServerError {
				errorResponse.Message = "Internal service error" // Hide the internal message error from client
			}
			// terminate the request here
			writeResponse(logger, w, r, err.StatusCode, errorResponse)
			return
		}

		if result == nil || http.StatusText(result.Status) == "" {
			// terminate the request here
			writeResponse(logger, w, r, http.StatusInternalServerError, newInternalServiceError())
			return
		}

		writeResponse(logger, w, r, result.Status, result.Data)
	}
}

// Write and return response
func writeResponse(
	logger *slog.Logger,
	w http.ResponseWriter,
	r *http.Request,
	statusCode int,
	res interface{},
) {
	respBytes, err := json.Marshal(res)

	if err != nil {
		http.Error(w, "Failed to process the request. Please try again later.", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	w.Write(respBytes) // nolint:errcheck
}

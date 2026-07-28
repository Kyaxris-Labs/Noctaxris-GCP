package gcperrors

import (
	"encoding/json"
	"net/http"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Status names align with google.rpc.Code JSON mapping used by Google APIs.
const (
	StatusOK                 = "OK"
	StatusCancelled          = "CANCELLED"
	StatusUnknown            = "UNKNOWN"
	StatusInvalidArgument    = "INVALID_ARGUMENT"
	StatusDeadlineExceeded   = "DEADLINE_EXCEEDED"
	StatusNotFound           = "NOT_FOUND"
	StatusAlreadyExists      = "ALREADY_EXISTS"
	StatusPermissionDenied   = "PERMISSION_DENIED"
	StatusResourceExhausted  = "RESOURCE_EXHAUSTED"
	StatusFailedPrecondition = "FAILED_PRECONDITION"
	StatusAborted            = "ABORTED"
	StatusOutOfRange         = "OUT_OF_RANGE"
	StatusUnimplemented      = "UNIMPLEMENTED"
	StatusInternal           = "INTERNAL"
	StatusUnavailable        = "UNAVAILABLE"
	StatusDataLoss           = "DATA_LOSS"
	StatusUnauthenticated    = "UNAUTHENTICATED"
)

// ErrorBody is the Google JSON error envelope for REST responses.
type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail is the nested error object.
type ErrorDetail struct {
	Code    int               `json:"code"`
	Message string            `json:"message"`
	Status  string            `json:"status"`
	Details []json.RawMessage `json:"details,omitempty"`
}

// WriteREST writes a Google-style JSON error to w.
func WriteREST(w http.ResponseWriter, httpCode int, statusName, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(httpCode)
	_ = json.NewEncoder(w).Encode(ErrorBody{Error: ErrorDetail{
		Code:    httpCode,
		Message: message,
		Status:  statusName,
	}})
}

// Unauthenticated writes HTTP 401 UNAUTHENTICATED.
func Unauthenticated(w http.ResponseWriter, message string) {
	if message == "" {
		message = "Request is missing required authentication credential. Expected OAuth 2 access token, login cookie or other valid authentication credential."
	}
	WriteREST(w, http.StatusUnauthorized, StatusUnauthenticated, message)
}

// PermissionDenied writes HTTP 403 PERMISSION_DENIED.
func PermissionDenied(w http.ResponseWriter, message string) {
	if message == "" {
		message = "The caller does not have permission."
	}
	WriteREST(w, http.StatusForbidden, StatusPermissionDenied, message)
}

// NotFound writes HTTP 404 NOT_FOUND.
func NotFound(w http.ResponseWriter, message string) {
	WriteREST(w, http.StatusNotFound, StatusNotFound, message)
}

// InvalidArgument writes HTTP 400 INVALID_ARGUMENT.
func InvalidArgument(w http.ResponseWriter, message string) {
	WriteREST(w, http.StatusBadRequest, StatusInvalidArgument, message)
}

// GRPC maps a Google status name and message to a gRPC status error.
func GRPC(statusName, message string) error {
	return status.Error(ToGRPCCode(statusName), message)
}

// ToGRPCCode maps a Google JSON status name to codes.Code.
func ToGRPCCode(statusName string) codes.Code {
	switch statusName {
	case StatusOK:
		return codes.OK
	case StatusCancelled:
		return codes.Canceled
	case StatusInvalidArgument:
		return codes.InvalidArgument
	case StatusDeadlineExceeded:
		return codes.DeadlineExceeded
	case StatusNotFound:
		return codes.NotFound
	case StatusAlreadyExists:
		return codes.AlreadyExists
	case StatusPermissionDenied:
		return codes.PermissionDenied
	case StatusResourceExhausted:
		return codes.ResourceExhausted
	case StatusFailedPrecondition:
		return codes.FailedPrecondition
	case StatusAborted:
		return codes.Aborted
	case StatusOutOfRange:
		return codes.OutOfRange
	case StatusUnimplemented:
		return codes.Unimplemented
	case StatusInternal:
		return codes.Internal
	case StatusUnavailable:
		return codes.Unavailable
	case StatusDataLoss:
		return codes.DataLoss
	case StatusUnauthenticated:
		return codes.Unauthenticated
	default:
		return codes.Unknown
	}
}

// HTTPStatusFor maps a Google status name to an HTTP status code.
func HTTPStatusFor(statusName string) int {
	switch statusName {
	case StatusOK:
		return http.StatusOK
	case StatusInvalidArgument, StatusFailedPrecondition, StatusOutOfRange:
		return http.StatusBadRequest
	case StatusUnauthenticated:
		return http.StatusUnauthorized
	case StatusPermissionDenied:
		return http.StatusForbidden
	case StatusNotFound:
		return http.StatusNotFound
	case StatusAlreadyExists, StatusAborted:
		return http.StatusConflict
	case StatusResourceExhausted:
		return http.StatusTooManyRequests
	case StatusCancelled:
		return 499
	case StatusUnimplemented:
		return http.StatusNotImplemented
	case StatusUnavailable:
		return http.StatusServiceUnavailable
	case StatusDeadlineExceeded:
		return http.StatusGatewayTimeout
	default:
		return http.StatusInternalServerError
	}
}

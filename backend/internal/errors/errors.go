package errors

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Error code constants
const (
	ErrNotFound           = "NOT_FOUND"
	ErrBadRequest         = "BAD_REQUEST"
	ErrUnauthorized       = "UNAUTHORIZED"
	ErrForbidden          = "FORBIDDEN"
	ErrInternalServer     = "INTERNAL_SERVER_ERROR"
	ErrServiceUnavailable = "SERVICE_UNAVAILABLE"
	ErrTimeout            = "TIMEOUT"
	ErrConflict           = "CONFLICT"
)

// AppError represents a structured application error.
type AppError struct {
	Code       string      `json:"code"`
	Message    string      `json:"message"`
	Details    interface{} `json:"details,omitempty"`
	HTTPStatus int         `json:"-"`
}

// Error implements the error interface.
func (e *AppError) Error() string {
	return e.Message
}

// WithDetails returns a copy of the AppError with additional details.
func (e *AppError) WithDetails(details interface{}) *AppError {
	return &AppError{
		Code:       e.Code,
		Message:    e.Message,
		Details:    details,
		HTTPStatus: e.HTTPStatus,
	}
}

// NewAppError creates a new AppError with the given code, message, and HTTP status.
func NewAppError(code, message string, status int) *AppError {
	return &AppError{
		Code:       code,
		Message:    message,
		HTTPStatus: status,
	}
}

// errorResponseBody is the JSON envelope for error responses.
type errorResponseBody struct {
	Error errorPayload `json:"error"`
}

type errorPayload struct {
	Code      string      `json:"code"`
	Message   string      `json:"message"`
	Details   interface{} `json:"details,omitempty"`
	RequestID string      `json:"request_id,omitempty"`
}

// ErrorResponse writes a structured JSON error response to the Gin context.
// It extracts the request_id from the context if available.
func ErrorResponse(c *gin.Context, err *AppError) {
	requestID, _ := c.Get("request_id")
	reqIDStr, _ := requestID.(string)

	c.JSON(err.HTTPStatus, errorResponseBody{
		Error: errorPayload{
			Code:      err.Code,
			Message:   err.Message,
			Details:   err.Details,
			RequestID: reqIDStr,
		},
	})
}

// Convenience constructors for common error types.

func NotFound(message string) *AppError {
	return NewAppError(ErrNotFound, message, http.StatusNotFound)
}

func BadRequest(message string) *AppError {
	return NewAppError(ErrBadRequest, message, http.StatusBadRequest)
}

func Unauthorized(message string) *AppError {
	return NewAppError(ErrUnauthorized, message, http.StatusUnauthorized)
}

func Forbidden(message string) *AppError {
	return NewAppError(ErrForbidden, message, http.StatusForbidden)
}

func InternalServer(message string) *AppError {
	return NewAppError(ErrInternalServer, message, http.StatusInternalServerError)
}

func ServiceUnavailable(message string) *AppError {
	return NewAppError(ErrServiceUnavailable, message, http.StatusServiceUnavailable)
}

func Timeout(message string) *AppError {
	return NewAppError(ErrTimeout, message, http.StatusGatewayTimeout)
}

func Conflict(message string) *AppError {
	return NewAppError(ErrConflict, message, http.StatusConflict)
}

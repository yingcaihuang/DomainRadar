package errors

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAppError(t *testing.T) {
	err := NewAppError(ErrNotFound, "resource not found", http.StatusNotFound)

	assert.Equal(t, ErrNotFound, err.Code)
	assert.Equal(t, "resource not found", err.Message)
	assert.Equal(t, http.StatusNotFound, err.HTTPStatus)
	assert.Nil(t, err.Details)
}

func TestAppError_Error(t *testing.T) {
	err := NewAppError(ErrBadRequest, "invalid input", http.StatusBadRequest)
	assert.Equal(t, "invalid input", err.Error())
}

func TestAppError_WithDetails(t *testing.T) {
	original := NewAppError(ErrBadRequest, "validation failed", http.StatusBadRequest)
	details := map[string]string{"field": "email", "reason": "invalid format"}

	withDetails := original.WithDetails(details)

	assert.Equal(t, original.Code, withDetails.Code)
	assert.Equal(t, original.Message, withDetails.Message)
	assert.Equal(t, original.HTTPStatus, withDetails.HTTPStatus)
	assert.Equal(t, details, withDetails.Details)
	// Original should be unchanged
	assert.Nil(t, original.Details)
}

func TestErrorResponse_WithRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("request_id", "req-12345")

	appErr := NewAppError(ErrNotFound, "domain not found", http.StatusNotFound)
	ErrorResponse(c, appErr)

	assert.Equal(t, http.StatusNotFound, w.Code)

	var body errorResponseBody
	err := json.Unmarshal(w.Body.Bytes(), &body)
	require.NoError(t, err)

	assert.Equal(t, ErrNotFound, body.Error.Code)
	assert.Equal(t, "domain not found", body.Error.Message)
	assert.Equal(t, "req-12345", body.Error.RequestID)
	assert.Nil(t, body.Error.Details)
}

func TestErrorResponse_WithoutRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	appErr := NewAppError(ErrInternalServer, "something went wrong", http.StatusInternalServerError)
	ErrorResponse(c, appErr)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var body errorResponseBody
	err := json.Unmarshal(w.Body.Bytes(), &body)
	require.NoError(t, err)

	assert.Equal(t, ErrInternalServer, body.Error.Code)
	assert.Equal(t, "something went wrong", body.Error.Message)
	assert.Empty(t, body.Error.RequestID)
}

func TestErrorResponse_WithDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("request_id", "req-99")

	details := map[string]string{"field": "name", "reason": "required"}
	appErr := NewAppError(ErrBadRequest, "validation error", http.StatusBadRequest).WithDetails(details)
	ErrorResponse(c, appErr)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var body map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &body)
	require.NoError(t, err)

	errObj := body["error"].(map[string]interface{})
	assert.Equal(t, ErrBadRequest, errObj["code"])
	assert.Equal(t, "validation error", errObj["message"])
	assert.Equal(t, "req-99", errObj["request_id"])

	detailsObj := errObj["details"].(map[string]interface{})
	assert.Equal(t, "name", detailsObj["field"])
	assert.Equal(t, "required", detailsObj["reason"])
}

func TestConvenienceConstructors(t *testing.T) {
	tests := []struct {
		name       string
		fn         func(string) *AppError
		code       string
		httpStatus int
	}{
		{"NotFound", NotFound, ErrNotFound, http.StatusNotFound},
		{"BadRequest", BadRequest, ErrBadRequest, http.StatusBadRequest},
		{"Unauthorized", Unauthorized, ErrUnauthorized, http.StatusUnauthorized},
		{"Forbidden", Forbidden, ErrForbidden, http.StatusForbidden},
		{"InternalServer", InternalServer, ErrInternalServer, http.StatusInternalServerError},
		{"ServiceUnavailable", ServiceUnavailable, ErrServiceUnavailable, http.StatusServiceUnavailable},
		{"Timeout", Timeout, ErrTimeout, http.StatusGatewayTimeout},
		{"Conflict", Conflict, ErrConflict, http.StatusConflict},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.fn("test message")
			assert.Equal(t, tt.code, err.Code)
			assert.Equal(t, "test message", err.Message)
			assert.Equal(t, tt.httpStatus, err.HTTPStatus)
		})
	}
}

func TestErrorCodes_AreDistinct(t *testing.T) {
	codes := []string{
		ErrNotFound,
		ErrBadRequest,
		ErrUnauthorized,
		ErrForbidden,
		ErrInternalServer,
		ErrServiceUnavailable,
		ErrTimeout,
		ErrConflict,
	}

	seen := make(map[string]bool)
	for _, code := range codes {
		assert.False(t, seen[code], "duplicate error code: %s", code)
		seen[code] = true
	}
}

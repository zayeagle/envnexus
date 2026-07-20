package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/oklog/ulid/v2"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
)

type APIResponse struct {
	RequestID string      `json:"request_id"`
	Data      interface{} `json:"data"`
	Error     *APIError   `json:"error"`
}

type APIError struct {
	Code    string      `json:"code"`
	Message string      `json:"message"`
	Details interface{} `json:"details,omitempty"`
}

func NewRequestID() string {
	return ulid.Make().String()
}

func RespondSuccess(c *gin.Context, status int, data interface{}) {
	c.JSON(status, APIResponse{
		RequestID: NewRequestID(),
		Data:      data,
		Error:     nil,
	})
}

func RespondError(c *gin.Context, err error) {
	if appErr, ok := err.(*domain.AppError); ok {
		c.JSON(appErr.HTTPStatus, APIResponse{
			RequestID: NewRequestID(),
			Data:      nil,
			Error: &APIError{
				Code:    appErr.Code,
				Message: appErr.Message,
			},
		})
		return
	}
	c.JSON(500, APIResponse{
		RequestID: NewRequestID(),
		Data:      nil,
		Error: &APIError{
			Code:    "internal_error",
			Message: err.Error(),
		},
	})
}

func RespondValidationError(c *gin.Context, message string) {
	c.JSON(400, APIResponse{
		RequestID: NewRequestID(),
		Data:      nil,
		Error: &APIError{
			Code:    "invalid_request",
			Message: message,
		},
	})
}

func ErrUnauthorizedFromContext() *domain.AppError {
	return domain.ErrUnauthorized
}

// RespondErrorCode responds with a manual error code and message (no AppError wrapping).
func RespondErrorCode(c *gin.Context, status int, code, message string) {
	c.JSON(status, APIResponse{
		RequestID: NewRequestID(),
		Data:      nil,
		Error:     &APIError{Code: code, Message: message},
	})
}

package proxy_io

import (
	"errors"
	"time"
)

// Коды ошибок beta-API (поле "code" в problem+json). Ветвиться нужно по ним,
// а не по HTTP-статусу и не по Detail.
const (
	CodeValidationFailed       = "validation_failed"
	CodeInvalidAPIKey          = "invalid_api_key"
	CodeNotFound               = "not_found"
	CodeInsufficientFunds      = "insufficient_funds"
	CodeIdempotencyKeyConflict = "idempotency_key_conflict"
	CodeIdempotencyKeyInProg   = "idempotency_key_in_progress"
	CodeProductUnavailable     = "product_unavailable"
	CodeOrderStateConflict     = "order_state_conflict"
	CodeRateLimited            = "rate_limited"
	CodeInternalError          = "internal_error"
	CodeTemporarilyUnavailable = "temporarily_unavailable"
)

// APIError — ошибка в формате RFC 9457 (application/problem+json).
type APIError struct {
	Type      string              `json:"type"`
	Title     string              `json:"title"`
	Status    int                 `json:"status"`
	Detail    string              `json:"detail"`
	Code      string              `json:"code"`
	RequestID string              `json:"request_id"`
	Instance  string              `json:"instance"`
	Errors    map[string][]string `json:"errors,omitempty"` // validation_failed

	// insufficient_funds
	Balance *struct {
		Available string `json:"available"`
		Currency  string `json:"currency"`
	} `json:"balance,omitempty"`
	RequiredAmount string `json:"required_amount,omitempty"`

	// Заполняется клиентом из заголовка Retry-After (429).
	RetryAfter time.Duration `json:"-"`
}

func (e *APIError) Error() string {
	msg := e.Code
	if msg == "" {
		msg = e.Title
	}
	s := "proxys: " + msg
	if e.Detail != "" {
		s += ": " + e.Detail
	}
	if e.RequestID != "" {
		s += " (request_id=" + e.RequestID + ")"
	}
	return s
}

// AsAPIError достаёт *APIError из цепочки ошибок.
func AsAPIError(err error) (*APIError, bool) {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr, true
	}
	return nil, false
}

// HasCode сообщает, что err — APIError с указанным кодом.
func HasCode(err error, code string) bool {
	apiErr, ok := AsAPIError(err)
	return ok && apiErr.Code == code
}

var (
	// ErrActivationFailed: заказ откатился из PENDING обратно в PAID.
	// Автоповтора нет, активирует оператор — нужна поддержка.
	ErrActivationFailed = errors.New("proxys: activation failed (order fell back to PAID), contact support")

	// ErrOrderInactive: заказ в терминальном состоянии (EXPIRED/RETURNED).
	ErrOrderInactive = errors.New("proxys: order is not active")

	// ErrIdempotencyKeyRequired: для платных операций ключ обязателен.
	ErrIdempotencyKeyRequired = errors.New("proxys: idempotency key is required for paid operations")
)

package proxy_io

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL — базовый URL из Quick start. Переопределяется WithBaseURL.
const DefaultBaseURL = "https://proxys.io/en/api/v3-beta"

const userAgent = "proxys-go-client/0.1"

// Client — клиент Proxys.io Client API v3-beta.
// Ключ аккаунта списывает баланс без подтверждения: храни его как платёжный секрет.
type Client struct {
	baseURL    string
	apiKey     string
	http       *http.Client
	maxRetries int

	ISPStatic *ISPStaticService
}

type Option func(*Client)

func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") }
}

func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http = h }
}

// WithMaxRetries — сколько раз повторять безопасные запросы (429, 503,
// idempotency_key_in_progress, сетевые сбои). 0 отключает ретраи.
func WithMaxRetries(n int) Option {
	return func(c *Client) { c.maxRetries = n }
}

func NewClient(apiKey string, opts ...Option) *Client {
	c := &Client{
		baseURL:    DefaultBaseURL,
		apiKey:     apiKey,
		http:       &http.Client{Timeout: 30 * time.Second},
		maxRetries: 3,
	}
	for _, o := range opts {
		o(c)
	}
	c.ISPStatic = &ISPStaticService{c: c}
	return c
}

// Balance — GET /balance.
func (c *Client) Balance(ctx context.Context) (*BalanceResponse, error) {
	return do[BalanceResponse](ctx, c, request{method: http.MethodGet, path: "/balance"})
}

// NewIdempotencyKey возвращает UUID v4 для заголовка Idempotency-Key.
// Сохрани ключ до отправки запроса и при повторе используй тот же.
func NewIdempotencyKey() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("proxys: crypto/rand failed: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// ---- внутренности ----

type request struct {
	method  string
	path    string
	query   url.Values
	body    any
	idemKey string // непустой — запрос платный и безопасен для повтора
}

func do[T any](ctx context.Context, c *Client, r request) (*T, error) {
	var payload []byte
	if r.body != nil {
		var err error
		if payload, err = json.Marshal(r.body); err != nil {
			return nil, fmt.Errorf("proxys: marshal request: %w", err)
		}
	}
	u := c.baseURL + r.path
	if len(r.query) > 0 {
		u += "?" + r.query.Encode()
	}

	// Не-GET без Idempotency-Key повторять нельзя: можно создать второй платный заказ.
	retryable := r.method == http.MethodGet || r.idemKey != ""

	for attempt := 0; ; attempt++ {
		out, err := doOnce[T](ctx, c, r, u, payload)
		if err == nil {
			return out, nil
		}
		delay, ok := retryDelay(ctx, err, attempt)
		if !ok || !retryable || attempt >= c.maxRetries {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
}

func doOnce[T any](ctx context.Context, c *Client, r request, u string, payload []byte) (*T, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, r.method, u, body)
	if err != nil {
		return nil, fmt.Errorf("proxys: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey) // только заголовок, не query
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if r.idemKey != "" {
		req.Header.Set("Idempotency-Key", r.idemKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, parseAPIError(resp)
	}

	var out T
	// Намеренно без DisallowUnknownFields: контракт beta может расширяться.
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&out); err != nil {
		return nil, fmt.Errorf("proxys: decode response (status %d): %w", resp.StatusCode, err)
	}
	return &out, nil
}

func parseAPIError(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	apiErr := &APIError{}
	if err := json.Unmarshal(raw, apiErr); err != nil || apiErr.Code == "" {
		// не problem+json (например, ошибка прокси/балансировщика)
		snippet := strings.TrimSpace(string(raw))
		if len(snippet) > 300 {
			snippet = snippet[:300]
		}
		apiErr = &APIError{Title: http.StatusText(resp.StatusCode), Detail: snippet}
	}
	if apiErr.Status == 0 {
		apiErr.Status = resp.StatusCode
	}
	if apiErr.RequestID == "" {
		apiErr.RequestID = resp.Header.Get("X-Request-Id")
	}
	if ra := resp.Header.Get("Retry-After"); ra != "" {
		if sec, err := strconv.Atoi(strings.TrimSpace(ra)); err == nil && sec >= 0 {
			apiErr.RetryAfter = time.Duration(sec) * time.Second
		}
	}
	return apiErr
}

// retryDelay решает, можно ли повторить запрос, и через сколько.
func retryDelay(ctx context.Context, err error, attempt int) (time.Duration, bool) {
	if ctx.Err() != nil {
		return 0, false
	}
	backoff := 500 * time.Millisecond << attempt
	if backoff > 8*time.Second {
		backoff = 8 * time.Second
	}

	var apiErr *APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.Code == CodeRateLimited || apiErr.Status == http.StatusTooManyRequests:
			if apiErr.RetryAfter > 0 {
				return apiErr.RetryAfter, true
			}
			return backoff, true
		case apiErr.Code == CodeTemporarilyUnavailable || apiErr.Status == http.StatusServiceUnavailable:
			return backoff, true // по доке: деньги не списаны, повтор безопасен
		case apiErr.Code == CodeIdempotencyKeyInProg:
			return backoff, true // позже, с тем же ключом
		}
		return 0, false // 400/401/404/422/409 funds/500 — повтор бессмысленен
	}
	// Сетевая ошибка / таймаут: для GET и запросов с Idempotency-Key безопасно.
	return backoff, true
}

package proxy_io

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// ISPStaticService — группа /isp-static (IP × календарные месяцы).
type ISPStaticService struct {
	c *Client
}

// Validate проверяет тело запроса до отправки (сервер проверит ещё раз).
func (r IspStaticOrderRequest) Validate() error {
	if r.Count < 1 || r.Count > 100 {
		return fmt.Errorf("proxys: count must be 1..100, got %d", r.Count)
	}
	if r.Country == "" || len(r.Country) > 64 {
		return errors.New("proxys: country (id from Countries) is required, max 64 chars")
	}
	switch r.Period {
	case 30, 60, 90:
	default:
		return fmt.Errorf("proxys: period must be 30, 60 or 90, got %d", r.Period)
	}
	if r.Coupon != nil && len(*r.Coupon) > 50 {
		return errors.New("proxys: coupon max 50 chars")
	}
	return nil
}

// Tariffs — GET /isp-static/tariffs.
func (s *ISPStaticService) Tariffs(ctx context.Context) (*TariffsResponse, error) {
	return do[TariffsResponse](ctx, s.c, request{method: http.MethodGet, path: "/isp-static/tariffs"})
}

// Price — POST /isp-static/price. Деньги не списываются, заказ не создаётся.
func (s *ISPStaticService) Price(ctx context.Context, req IspStaticOrderRequest) (*PriceQuote, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	return do[PriceQuote](ctx, s.c, request{method: http.MethodPost, path: "/isp-static/price", body: req})
}

// Countries — GET /isp-static/countries. Для покупки используй Country.ID.
func (s *ISPStaticService) Countries(ctx context.Context) ([]Country, error) {
	out, err := do[[]Country](ctx, s.c, request{method: http.MethodGet, path: "/isp-static/countries"})
	if err != nil {
		return nil, err
	}
	return *out, nil
}

// OrdersFilter — параметры GET /isp-static/orders. Нулевые значения не отправляются.
type OrdersFilter struct {
	Status OrderStatus // пусто — все статусы, включая EXPIRED и RETURNED
	Page   int         // с 1
	Limit  int         // 1..100, по умолчанию 20
}

// Orders — GET /isp-static/orders.
func (s *ISPStaticService) Orders(ctx context.Context, f OrdersFilter) (*OrdersPage, error) {
	q := url.Values{}
	if f.Status != "" {
		q.Set("status", string(f.Status))
	}
	if f.Page > 0 {
		q.Set("page", strconv.Itoa(f.Page))
	}
	if f.Limit > 0 {
		q.Set("limit", strconv.Itoa(f.Limit))
	}
	return do[OrdersPage](ctx, s.c, request{method: http.MethodGet, path: "/isp-static/orders", query: q})
}

// Buy — POST /isp-static/orders. СПИСЫВАЕТ деньги сразу.
//
// idemKey обязателен (см. NewIdempotencyKey): сохрани его до вызова и при повторе
// после таймаута или падения процесса используй тот же — тогда второго заказа не будет.
// Ответ не означает, что прокси выданы: дождись IN_WORK (WaitActive).
func (s *ISPStaticService) Buy(ctx context.Context, req IspStaticOrderRequest, idemKey string) (*Order, error) {
	if idemKey == "" {
		return nil, ErrIdempotencyKeyRequired
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	return do[Order](ctx, s.c, request{
		method: http.MethodPost, path: "/isp-static/orders", body: req, idemKey: idemKey,
	})
}

// Order — GET /isp-static/orders/{orderId}.
func (s *ISPStaticService) Order(ctx context.Context, orderID int64) (*Order, error) {
	return do[Order](ctx, s.c, request{
		method: http.MethodGet, path: fmt.Sprintf("/isp-static/orders/%d", orderID),
	})
}

// Renew — POST /isp-static/orders/{orderId}/renew. СПИСЫВАЕТ деньги.
// Продлевает ровно на 1 календарный месяц, только активный заказ. Тела нет.
func (s *ISPStaticService) Renew(ctx context.Context, orderID int64, idemKey string) (*Order, error) {
	if idemKey == "" {
		return nil, ErrIdempotencyKeyRequired
	}
	return do[Order](ctx, s.c, request{
		method: http.MethodPost, path: fmt.Sprintf("/isp-static/orders/%d/renew", orderID), idemKey: idemKey,
	})
}

// Proxies — GET /isp-static/orders/{orderId}/proxies.
// Пока заказ PAID/PENDING, сервер отвечает 422 (order_state_conflict), а не пустым списком.
func (s *ISPStaticService) Proxies(ctx context.Context, orderID int64) ([]Proxy, error) {
	out, err := do[[]Proxy](ctx, s.c, request{
		method: http.MethodGet, path: fmt.Sprintf("/isp-static/orders/%d/proxies", orderID),
	})
	if err != nil {
		return nil, err
	}
	return *out, nil
}

// SetAutoRenewal — PUT /isp-static/orders/{orderId}/auto-renewal.
// Возвращает фактическое состояние после чтения, а не то, что отправлено.
func (s *ISPStaticService) SetAutoRenewal(ctx context.Context, orderID int64, enabled bool) (bool, error) {
	out, err := do[AutoRenewalResponse](ctx, s.c, request{
		method: http.MethodPut,
		path:   fmt.Sprintf("/isp-static/orders/%d/auto-renewal", orderID),
		body:   AutoRenewalRequest{Enabled: enabled},
	})
	if err != nil {
		return false, err
	}
	return out.AutoRenewal, nil
}

// WaitActive опрашивает заказ, пока он не станет IN_WORK. Общий таймаут задаётся ctx
// (provisioning занимает минуты, ставь 10–15 минут). interval <= 0 даёт 10 секунд.
//
// ErrActivationFailed: заказ откатился из PENDING в PAID (автоповтора нет).
// ErrOrderInactive: заказ EXPIRED или RETURNED.
func (s *ISPStaticService) WaitActive(ctx context.Context, orderID int64, interval time.Duration) (*Order, error) {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	sawPending := false
	for {
		o, err := s.Order(ctx, orderID)
		if err != nil {
			return nil, err
		}
		switch o.Status {
		case StatusInWork:
			return o, nil
		case StatusExpired, StatusReturned:
			return o, fmt.Errorf("%w: status %s", ErrOrderInactive, o.Status)
		case StatusPending:
			sawPending = true
		case StatusPaid:
			if sawPending {
				return o, ErrActivationFailed
			}
		case StatusWait:
			// выставлено оператором вручную: заказ обрабатывается, ждём
		}
		select {
		case <-ctx.Done():
			return o, ctx.Err()
		case <-time.After(interval):
		}
	}
}

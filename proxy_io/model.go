package proxy_io

import (
	"encoding/json"
	"time"
)

// Money: value в ответах приходит числом, а в ошибках — строкой.
// json.Number принимает оба варианта и не теряет точность.
type Money struct {
	Value    json.Number `json:"value"`
	Currency string      `json:"currency"`
}

// GET /balance
type BalanceResponse struct {
	Main    Money  `json:"main"`
	MainRub *Money `json:"main_in_rub,omitempty"`
}

// ---- tariffs ----

type TariffsResponse struct {
	Product  string       `json:"product"`
	Currency string       `json:"currency"`
	Unit     string       `json:"unit"`
	Steps    []TariffStep `json:"steps"`
}

type TariffStep struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Min       int    `json:"min"`
	Max       int    `json:"max"`
	Price     Money  `json:"price"` // за 1 IP на 30 дней
	PriceNote string `json:"price_note"`
}

// ---- price / buy ----

// IspStaticOrderRequest — общее тело для POST /price и POST /orders.
type IspStaticOrderRequest struct {
	Count   int     `json:"count"`            // 1..100
	Country string  `json:"country"`          // Country.ID, не ISO-код
	Period  int     `json:"period"`           // 30, 60 или 90
	Coupon  *string `json:"coupon,omitempty"` // nullable
}

type PriceQuote struct {
	Amount                Money `json:"amount"`
	AmountWithoutDiscount Money `json:"amount_without_discount"`
	DiscountPercent       int   `json:"discount_percent"`
	TariffStepID          int   `json:"tariff_step_id"`
}

// ---- countries ----

type Country struct {
	ID   string `json:"id"` // его и передаём при покупке
	Name string `json:"name"`
	Code string `json:"code"`
}

// ---- orders ----

type OrderStatus string

const (
	StatusPaid     OrderStatus = "PAID"
	StatusPending  OrderStatus = "PENDING"
	StatusInWork   OrderStatus = "IN_WORK"
	StatusWait     OrderStatus = "WAIT" // выставляет оператор вручную
	StatusExpired  OrderStatus = "EXPIRED"
	StatusReturned OrderStatus = "RETURNED"
)

type Order struct {
	ID             int64        `json:"id"`
	Status         OrderStatus  `json:"status"`
	CreatedAt      time.Time    `json:"created_at"`
	ExpiresAt      time.Time    `json:"expires_at"`
	PurchaseAmount Money        `json:"purchase_amount"`
	RenewalAmount  Money        `json:"renewal_amount"`
	Period         int          `json:"period"`
	AutoRenewal    bool         `json:"auto_renewal"`
	Proxies        OrderProxies `json:"proxies"`
}

type OrderProxies struct {
	Count        int    `json:"count"`
	Country      string `json:"country"`
	ProxiesReady bool   `json:"proxies_ready"`
}

type OrdersPage struct {
	Data       []Order    `json:"data"`
	Pagination Pagination `json:"pagination"`
}

type Pagination struct {
	TotalRecords int  `json:"total_records"`
	CurrentPage  int  `json:"current_page"`
	TotalPages   int  `json:"total_pages"`
	NextPage     *int `json:"next_page"` // null на краю
	PrevPage     *int `json:"prev_page"`
}

// ---- credentials ----

type Proxy struct {
	IP        string `json:"ip"`
	PortHTTP  int    `json:"port_http"`
	PortSocks int    `json:"port_socks"`
	Login     string `json:"login"`
	Password  string `json:"password"`
	Country   string `json:"country"`
}

// ---- auto-renewal ----

type AutoRenewalRequest struct {
	Enabled bool `json:"enabled"` // только настоящий bool
}

type AutoRenewalResponse struct {
	AutoRenewal bool `json:"auto_renewal"` // фактическое состояние после чтения
}

package provision

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"go_tg_hold/internal/config"
	"go_tg_hold/internal/sheet"
	"go_tg_hold/proxy_io"
)

// pendingOrder хранится на диске ДО вызова Buy, чтобы при падении процесса
// повторить покупку с тем же Idempotency-Key и не купить второй раз.
type pendingOrder struct {
	IdemKey   string                         `json:"idem_key"`
	Request   proxy_io.IspStaticOrderRequest `json:"request"`
	OrderID   int64                          `json:"order_id,omitempty"`
	Failed    bool                           `json:"failed,omitempty"`
	CreatedAt time.Time                      `json:"created_at"`
}

type Provisioner struct {
	cfg   *config.Config
	store *sheet.Store
	px    *proxy_io.Client
	in    *bufio.Reader
}

func New(cfg *config.Config, store *sheet.Store, px *proxy_io.Client, in *bufio.Reader) *Provisioner {
	return &Provisioner{cfg: cfg, store: store, px: px, in: in}
}

func (p *Provisioner) pendingPath() string {
	return filepath.Join(p.cfg.StateDir, "pending_order.json")
}

// Run покупает прокси для аккаунтов без прокси и записывает их в таблицу.
func (p *Provisioner) Run(ctx context.Context) error {
	accounts, err := p.store.Load(ctx)
	if err != nil {
		return err
	}
	var need []sheet.Account
	for _, a := range accounts {
		if !a.HasProxy() {
			need = append(need, a)
		}
	}

	pend, err := p.loadPending()
	if err != nil {
		return err
	}
	if pend != nil && pend.Failed {
		return fmt.Errorf("provision: order %d failed to activate earlier; contact Proxys.io support, "+
			"then delete %s to allow a new purchase", pend.OrderID, p.pendingPath())
	}
	if len(need) == 0 {
		if pend != nil {
			return fmt.Errorf("provision: unfinished order %d exists but every account already has a proxy; check %s",
				pend.OrderID, p.pendingPath())
		}
		fmt.Println("All accounts already have proxies, nothing to do.")
		return nil
	}

	proxiesNeeded := (len(need) + p.cfg.AccountsPerProxy - 1) / p.cfg.AccountsPerProxy

	switch {
	case pend == nil:
		if pend, err = p.startOrder(ctx, proxiesNeeded); err != nil {
			return err
		}
	case pend.OrderID == 0:
		fmt.Printf("Found an unfinished purchase (count=%d, country=%s, %d days) with idempotency key %s.\n",
			pend.Request.Count, pend.Request.Country, pend.Request.Period, pend.IdemKey)
		fmt.Println("Retrying with the same key cannot create a second order.")
		if !p.confirm("Retry the purchase?") {
			return errors.New("provision: cancelled")
		}
		if err := p.buy(ctx, pend); err != nil {
			return err
		}
	default:
		fmt.Printf("Resuming order %d.\n", pend.OrderID)
	}

	// Ждём активации (provisioning занимает минуты).
	wctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	fmt.Printf("Waiting for order %d to become active...\n", pend.OrderID)
	order, err := p.px.ISPStatic.WaitActive(wctx, pend.OrderID, 10*time.Second)
	if errors.Is(err, proxy_io.ErrActivationFailed) {
		pend.Failed = true
		_ = p.savePending(pend)
		for _, a := range need {
			_ = p.store.Update(ctx, a.Row, map[string]string{
				sheet.ColStatus:    sheet.StatusProxyFailed,
				sheet.ColOrderID:   strconv.FormatInt(pend.OrderID, 10),
				sheet.ColLastError: "activation failed, contact Proxys.io support",
			})
		}
		return err
	}
	if err != nil {
		// Файл заказа остаётся: повторный запуск продолжит ожидание, а не купит заново.
		return fmt.Errorf("provision: wait order %d: %w (run again to resume)", pend.OrderID, err)
	}

	proxies, err := p.px.ISPStatic.Proxies(ctx, pend.OrderID)
	if err != nil {
		return fmt.Errorf("provision: get proxies: %w", err)
	}
	if len(proxies) < proxiesNeeded {
		return fmt.Errorf("provision: order has %d proxies, need %d", len(proxies), proxiesNeeded)
	}
	// Стабильный порядок между запусками.
	sort.Slice(proxies, func(i, j int) bool { return proxies[i].IP < proxies[j].IP })

	expires := order.ExpiresAt.UTC().Format(time.RFC3339)
	for i, a := range need {
		px := proxies[i/p.cfg.AccountsPerProxy]
		err := p.store.Update(ctx, a.Row, map[string]string{
			sheet.ColProxyIP:      px.IP,
			sheet.ColProxyPort:    strconv.Itoa(px.PortSocks),
			sheet.ColProxyLogin:   px.Login,
			sheet.ColProxyPass:    px.Password,
			sheet.ColProxyExpires: expires,
			sheet.ColOrderID:      strconv.FormatInt(pend.OrderID, 10),
			sheet.ColStatus:       sheet.StatusProxyReady,
			sheet.ColLastError:    "",
		})
		if err != nil {
			// Файл заказа не удаляем: повторный запуск доберёт оставшиеся строки.
			return fmt.Errorf("provision: write proxy for %s: %w", a.Number, err)
		}
		fmt.Printf("  %s -> %s:%d\n", a.Number, px.IP, px.PortSocks)
	}

	if err := os.Remove(p.pendingPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	fmt.Printf("Done. Proxies expire at %s.\n", expires)
	return nil
}

func (p *Provisioner) startOrder(ctx context.Context, count int) (*pendingOrder, error) {
	countries, err := p.px.ISPStatic.Countries(ctx)
	if err != nil {
		return nil, fmt.Errorf("provision: countries: %w", err)
	}
	var country *proxy_io.Country
	for i := range countries {
		if strings.EqualFold(countries[i].Code, p.cfg.CountryCode) {
			country = &countries[i]
			break
		}
	}
	if country == nil {
		return nil, fmt.Errorf("provision: country %q not found among ISP static countries", p.cfg.CountryCode)
	}

	req := proxy_io.IspStaticOrderRequest{Count: count, Country: country.ID, Period: p.cfg.PeriodDays}
	quote, err := p.px.ISPStatic.Price(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("provision: price: %w", err)
	}
	bal, err := p.px.Balance(ctx)
	if err != nil {
		return nil, fmt.Errorf("provision: balance: %w", err)
	}

	fmt.Printf("\nPurchase: %d ISP static proxy(ies), %s (%s), %d days\n", count, country.Name, country.Code, req.Period)
	fmt.Printf("Price:    %s %s (discount %d%%)\n", quote.Amount.Value, quote.Amount.Currency, quote.DiscountPercent)
	fmt.Printf("Balance:  %s %s\n\n", bal.Main.Value, bal.Main.Currency)

	amount, err := quote.Amount.Value.Float64()
	if err != nil {
		return nil, fmt.Errorf("provision: bad price value %q: %w", quote.Amount.Value, err)
	}
	if p.cfg.MaxPrice > 0 && amount > p.cfg.MaxPrice {
		return nil, fmt.Errorf("provision: price %.2f exceeds max_price %.2f", amount, p.cfg.MaxPrice)
	}
	if have, err := bal.Main.Value.Float64(); err == nil && have < amount {
		return nil, fmt.Errorf("provision: insufficient balance (%.2f < %.2f)", have, amount)
	}
	if !p.confirm("Buy now? This charges your Proxys.io balance.") {
		return nil, errors.New("provision: cancelled")
	}

	pend := &pendingOrder{IdemKey: proxy_io.NewIdempotencyKey(), Request: req, CreatedAt: time.Now().UTC()}
	if err := p.savePending(pend); err != nil { // ключ на диске ДО покупки
		return nil, err
	}
	if err := p.buy(ctx, pend); err != nil {
		return nil, err
	}
	return pend, nil
}

func (p *Provisioner) buy(ctx context.Context, pend *pendingOrder) error {
	order, err := p.px.ISPStatic.Buy(ctx, pend.Request, pend.IdemKey)
	if err != nil {
		return fmt.Errorf("provision: buy: %w (state kept in %s; run again to retry safely)", err, p.pendingPath())
	}
	pend.OrderID = order.ID
	fmt.Printf("Order %d created (status %s).\n", order.ID, order.Status)
	return p.savePending(pend)
}

func (p *Provisioner) confirm(prompt string) bool {
	fmt.Print(prompt + " [y/N]: ")
	line, _ := p.in.ReadString('\n')
	return strings.EqualFold(strings.TrimSpace(line), "y")
}

func (p *Provisioner) loadPending() (*pendingOrder, error) {
	raw, err := os.ReadFile(p.pendingPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var pend pendingOrder
	if err := json.Unmarshal(raw, &pend); err != nil {
		return nil, fmt.Errorf("provision: corrupt %s: %w", p.pendingPath(), err)
	}
	return &pend, nil
}

func (p *Provisioner) savePending(pend *pendingOrder) error {
	if err := os.MkdirAll(p.cfg.StateDir, 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(pend, "", "  ")
	if err != nil {
		return err
	}
	tmp := p.pendingPath() + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p.pendingPath())
}

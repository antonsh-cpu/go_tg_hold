// Package tgclient собирает клиент gotd/td для аккаунта: прокси, сессия, устройство.
// Им пользуются и login, и profile, и все следующие модули, чтобы аккаунт
// всегда подключался одинаково (один IP, одно устройство, один файл сессии).
package tgclient

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/dcs"
	"github.com/gotd/td/tgerr"
	"golang.org/x/net/proxy"

	"go_tg_hold/internal/config"
	"go_tg_hold/internal/sheet"
)

// New создаёт клиент аккаунта. Параметры устройства должны быть уже заполнены
// (их создаёт login), иначе аккаунт каждый раз выглядел бы как новое устройство.
func New(cfg *config.Config, a sheet.Account) (*telegram.Client, error) {
	apiID, err := strconv.Atoi(a.APIID)
	if err != nil || a.APIHash == "" {
		return nil, errors.New("api_id / api_hash are empty or invalid")
	}
	if !a.HasProxy() {
		return nil, errors.New("account has no proxy")
	}
	if a.DeviceModel == "" || a.SystemVersion == "" || a.AppVersion == "" || a.LangCode == "" {
		return nil, errors.New("device parameters are empty (run login first)")
	}
	dial, err := Socks5Dial(a)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.SessionsDir, 0o700); err != nil {
		return nil, err
	}
	return telegram.NewClient(apiID, a.APIHash, telegram.Options{
		SessionStorage: &telegram.FileSessionStorage{
			Path: filepath.Join(cfg.SessionsDir, a.Digits()+".json"),
		},
		Resolver: dcs.Plain(dcs.PlainOptions{Dial: dial}),
		Device: telegram.DeviceConfig{
			DeviceModel:    a.DeviceModel,
			SystemVersion:  a.SystemVersion,
			AppVersion:     a.AppVersion,
			LangCode:       a.LangCode,
			SystemLangCode: "en-US",
		},
	}), nil
}

// Socks5Dial строит dial-функцию через постоянный прокси аккаунта.
func Socks5Dial(a sheet.Account) (func(ctx context.Context, network, addr string) (net.Conn, error), error) {
	d, err := proxy.SOCKS5("tcp", net.JoinHostPort(a.ProxyIP, a.ProxyPort),
		&proxy.Auth{User: a.ProxyLogin, Password: a.ProxyPass}, proxy.Direct)
	if err != nil {
		return nil, fmt.Errorf("socks5: %w", err)
	}
	if cd, ok := d.(proxy.ContextDialer); ok {
		return cd.DialContext, nil
	}
	return func(_ context.Context, network, addr string) (net.Conn, error) {
		return d.Dial(network, addr)
	}, nil
}

// Classify переводит ошибку Telegram в статус аккаунта для таблицы.
func Classify(err error) (status string, resumeAfter time.Time) {
	if d, ok := tgerr.AsFloodWait(err); ok {
		return sheet.StatusFloodWait, time.Now().Add(d)
	}
	switch {
	case tgerr.Is(err, "USER_DEACTIVATED_BAN", "PHONE_NUMBER_BANNED", "USER_DEACTIVATED"):
		return sheet.StatusBanned, time.Time{}
	case tgerr.Is(err, "AUTH_KEY_UNREGISTERED", "SESSION_REVOKED", "SESSION_EXPIRED", "AUTH_KEY_DUPLICATED"):
		return sheet.StatusAuthLost, time.Time{}
	}
	return sheet.StatusLoginFailed, time.Time{}
}

type Device struct{ Model, System, App, Lang string }

// NewDevice генерирует параметры устройства. Версию приложения периодически
// обновляйте до актуальной версии Telegram для Android.
func NewDevice() Device {
	models := []string{"Samsung Galaxy S23", "Samsung Galaxy A54", "Google Pixel 7", "Google Pixel 8", "Xiaomi 13", "OnePlus 11"}
	systems := []string{"Android 13", "Android 14"}
	return Device{
		Model:  models[rand.IntN(len(models))],
		System: systems[rand.IntN(len(systems))],
		App:    "10.14.5",
		Lang:   "en",
	}
}
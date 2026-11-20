package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

type Config struct {
	SpreadsheetID      string  `json:"spreadsheet_id"`
	SheetName          string  `json:"sheet_name"`
	ServiceAccountFile string  `json:"service_account_file"`
	CountryCode        string  `json:"country_code"`         // ISO-код, например US
	PeriodDays         int     `json:"period_days"`          // 30, 60 или 90
	AccountsPerProxy   int     `json:"accounts_per_proxy"`   // сколько аккаунтов на один прокси
	MaxPrice           float64 `json:"max_price"`            // 0 = без лимита
	SessionsDir        string  `json:"sessions_dir"`         // файлы сессий Telegram
	StateDir           string  `json:"state_dir"`            // служебные файлы (незавершённый заказ)
	PauseBetweenLogins int     `json:"pause_between_logins"` // секунды между входами

	ImagesDir            string `json:"images_dir"` // папка с аватарами (images/m, images/f или просто images)
	MinAge               int    `json:"min_age"`    // возраст для генерации даты рождения
	MaxAge               int    `json:"max_age"`
	PauseBetweenProfiles int    `json:"pause_between_profiles"` // секунды между аккаунтами при настройке профиля

	// Ключ Proxys.io списывает деньги без подтверждения, поэтому только из окружения.
	ProxysAPIKey string `json:"-"`
}

func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	cfg := &Config{
		SheetName:          "test-tg-auth",
		CountryCode:        "US",
		PeriodDays:         30,
		AccountsPerProxy:   2,
		SessionsDir:        "sessions",
		StateDir:           "state",
		PauseBetweenLogins: 30,

		ImagesDir:            "images",
		MinAge:               22,
		MaxAge:               40,
		PauseBetweenProfiles: 15,
	}
	if err := json.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}
	cfg.ProxysAPIKey = strings.TrimSpace(os.Getenv("PROXYS_API_KEY"))

	switch {
	case cfg.SpreadsheetID == "":
		return nil, errors.New("config: spreadsheet_id is required")
	case cfg.ServiceAccountFile == "":
		return nil, errors.New("config: service_account_file is required")
	case cfg.AccountsPerProxy < 1:
		return nil, errors.New("config: accounts_per_proxy must be >= 1")
	case cfg.MinAge < 18 || cfg.MaxAge < cfg.MinAge:
		return nil, errors.New("config: need 18 <= min_age <= max_age")
	case cfg.PeriodDays != 30 && cfg.PeriodDays != 60 && cfg.PeriodDays != 90:
		return nil, errors.New("config: period_days must be 30, 60 or 90")
	}
	return cfg, nil
}

package tglogin

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"

	"go_tg_hold/internal/config"
	"go_tg_hold/internal/sheet"
	"go_tg_hold/internal/tgclient"
)

type Logger struct {
	cfg   *config.Config
	store *sheet.Store
	in    *bufio.Reader
}

func New(cfg *config.Config, store *sheet.Store, in *bufio.Reader) *Logger {
	return &Logger{cfg: cfg, store: store, in: in}
}

// Run по очереди входит во все аккаунты, у которых есть прокси и которые ещё не active.
func (l *Logger) Run(ctx context.Context) error {
	accounts, err := l.store.Load(ctx)
	if err != nil {
		return err
	}

	var todo []sheet.Account
	var problems []string
	for _, a := range accounts {
		if a.Status == sheet.StatusActive || a.Status == sheet.StatusBanned {
			continue
		}
		if !a.HasProxy() {
			problems = append(problems, a.Number+": no proxy (run `provision` first)")
			continue
		}
		if _, err := strconv.Atoi(a.APIID); err != nil || a.APIHash == "" {
			problems = append(problems, a.Number+": api_id / api_hash are empty or invalid")
			continue
		}
		todo = append(todo, a)
	}
	// Любая ошибка конфигурации останавливает запуск до обращения к Telegram.
	if len(problems) > 0 {
		return fmt.Errorf("login: fix the sheet first:\n  %s", strings.Join(problems, "\n  "))
	}
	if len(todo) == 0 {
		fmt.Println("No accounts to log in.")
		return nil
	}

	for i, a := range todo {
		if i > 0 {
			pause := time.Duration(l.cfg.PauseBetweenLogins) * time.Second
			fmt.Printf("Pause %s before the next account...\n", pause)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(pause):
			}
		}
		fmt.Printf("\n=== %s (row %d) ===\n", a.Number, a.Row)
		if err := l.loginOne(ctx, a); err != nil {
			fmt.Printf("FAILED: %v\n", err)
			if ctx.Err() != nil {
				return ctx.Err()
			}
		}
	}
	return nil
}

func (l *Logger) loginOne(ctx context.Context, a sheet.Account) error {
	// Параметры устройства создаются один раз и дальше не меняются.
	if a.DeviceModel == "" || a.SystemVersion == "" || a.AppVersion == "" || a.LangCode == "" {
		d := tgclient.NewDevice()
		a.DeviceModel, a.SystemVersion, a.AppVersion, a.LangCode = d.Model, d.System, d.App, d.Lang
		if err := l.store.Update(ctx, a.Row, map[string]string{
			sheet.ColDeviceModel:   a.DeviceModel,
			sheet.ColSystemVersion: a.SystemVersion,
			sheet.ColAppVersion:    a.AppVersion,
			sheet.ColLangCode:      a.LangCode,
		}); err != nil {
			return err
		}
	}

	client, err := tgclient.New(l.cfg, a)
	if err != nil {
		return l.fail(ctx, a, err)
	}

	var usernameErr error
	runErr := client.Run(ctx, func(ctx context.Context) error {
		flow := auth.NewFlow(
			auth.Constant(a.Phone(), a.Password, auth.CodeAuthenticatorFunc(
				func(ctx context.Context, _ *tg.AuthSentCode) (string, error) {
					fmt.Printf("Enter the login code for %s: ", a.Phone())
					line, err := l.in.ReadString('\n')
					return strings.TrimSpace(line), err
				},
			)),
			auth.SendCodeOptions{},
		)
		if err := client.Auth().IfNecessary(ctx, flow); err != nil {
			return fmt.Errorf("auth: %w", err)
		}

		self, err := client.Self(ctx)
		if err != nil {
			return fmt.Errorf("self: %w", err)
		}
		fmt.Printf("Logged in as id=%d\n", self.ID)

		// Ошибка username не должна считаться провалом входа.
		if want := strings.TrimPrefix(a.Username, "@"); want != "" && self.Username != want {
			if _, err := client.API().AccountUpdateUsername(ctx, want); err != nil {
				usernameErr = err
			}
		}
		return nil
	})
	if runErr != nil {
		return l.fail(ctx, a, runErr)
	}

	lastErr := ""
	if usernameErr != nil {
		lastErr = "username: " + usernameErr.Error()
		fmt.Printf("Warning: %s\n", lastErr)
	}
	return l.store.Update(ctx, a.Row, map[string]string{
		sheet.ColStatus:      sheet.StatusActive,
		sheet.ColLastActive:  time.Now().UTC().Format(time.RFC3339),
		sheet.ColLastError:   trim(lastErr),
		sheet.ColResumeAfter: "",
	})
}

// fail записывает статус в таблицу и возвращает исходную ошибку.
func (l *Logger) fail(ctx context.Context, a sheet.Account, cause error) error {
	status, resume := tgclient.Classify(cause)
	fields := map[string]string{
		sheet.ColStatus:    status,
		sheet.ColLastError: trim(cause.Error()),
	}
	if !resume.IsZero() {
		fields[sheet.ColResumeAfter] = resume.UTC().Format(time.RFC3339)
	}
	if err := l.store.Update(ctx, a.Row, fields); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

func trim(s string) string {
	if len(s) > 300 {
		return s[:300]
	}
	return s
}

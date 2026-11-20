package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"go_tg_hold/internal/config"
	"go_tg_hold/internal/profile"
	"go_tg_hold/internal/provision"
	"go_tg_hold/internal/sheet"
	"go_tg_hold/internal/tglogin"
	"go_tg_hold/proxy_io"
)

const usage = `usage: tgctl [-config config.json] <command>

commands:
  provision   buy proxies for accounts without one and write them to the sheet
  login       log in to accounts (asks for the code in the console)
  profile     fill in profile: name, bio, birthday, photo, privacy
  all         provision, login, then profile
`

func main() {
	cfgPath := flag.String("config", "config.json", "path to config file")
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	flag.Parse()

	cmd := flag.Arg(0)
	if cmd != "provision" && cmd != "login" && cmd != "profile" && cmd != "all" {
		flag.Usage()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, *cfgPath, cmd); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfgPath, cmd string) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	store, err := sheet.New(ctx, cfg.ServiceAccountFile, cfg.SpreadsheetID, cfg.SheetName)
	if err != nil {
		return err
	}
	in := bufio.NewReader(os.Stdin) // один reader на весь процесс

	if cmd == "provision" || cmd == "all" {
		if cfg.ProxysAPIKey == "" {
			return errors.New("set PROXYS_API_KEY environment variable")
		}
		px := proxy_io.NewClient(cfg.ProxysAPIKey)
		if err := provision.New(cfg, store, px, in).Run(ctx); err != nil {
			return err
		}
	}
	if cmd == "login" || cmd == "all" {
		if err := tglogin.New(cfg, store, in).Run(ctx); err != nil {
			return err
		}
	}
	if cmd == "profile" || cmd == "all" {
		if err := profile.New(cfg, store).Run(ctx); err != nil {
			return err
		}
	}
	return nil
}

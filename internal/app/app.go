package app

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"eggbot/internal/bot"
	"eggbot/internal/config"
	"eggbot/internal/obs"
	"eggbot/internal/store"
	"eggbot/internal/userfile"
	"eggbot/internal/version"
)

type App struct {
	Cfg    *config.Config
	Log    *slog.Logger
	Bot    *bot.Bot
	Health *obs.Server
}

func (a *App) Run(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		a.Log.Info("shutting down")
		if a.Health != nil {
			a.Health.Stop()
		}
		a.Bot.Close()
	}()
	return a.Bot.Run()
}

func Main(args []string) int {
	fs := flag.NewFlagSet("eggbot", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	cfgPath := fs.String("c", "eggbot.toml", "config file")
	showVersion := fs.Bool("version", false, "print version and exit")
	checkConfig := fs.Bool("check-config", false, "validate config and exit")
	bootstrap := fs.String("bootstrap-owner", "", "set a configured owner password from EGGBOT_BOOTSTRAP_PASSWORD or stdin")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		fmt.Println(version.String())
		return 0
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		slog.Error("config", "err", err)
		return 1
	}
	if *checkConfig {
		fmt.Println("config ok")
		return 0
	}
	if *bootstrap != "" {
		if err := bootstrapOwner(cfg, *bootstrap); err != nil {
			slog.Error("bootstrap", "err", err)
			return 1
		}
		fmt.Println("owner password set")
		return 0
	}
	level := parseLevel(cfg.Log.Level)
	var log *slog.Logger
	if cfg.Observe.JSONLogs {
		log = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	} else {
		log = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	}

	b, err := bot.New(cfg, log)
	if err != nil {
		log.Error("init", "err", err)
		return 1
	}
	defer b.Close()

	health := &obs.Server{
		Listen: cfg.Observe.Listen,
		Log:    log,
		Status: obs.Status{
			Live:  func() bool { return true },
			Ready: b.Ready,
			Stats: func() map[string]any {
				return map[string]any{
					"irc_ready":   b.IRC.Ready(),
					"queue_depth": b.IRC.Queue().Len(),
					"queue_drops": b.IRC.Queue().Drops(),
					"irc_state":   b.IRC.State().String(),
				}
			},
		},
	}
	if err := health.Start(); err != nil {
		log.Error("health", "err", err)
		return 1
	}
	defer health.Stop()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	app := &App{Cfg: cfg, Log: log, Bot: b, Health: health}
	log.Info("starting", "nick", cfg.Nick, "server", cfg.ServerAddr(), "version", version.String())
	if err := app.Run(ctx); err != nil {
		log.Error("run", "err", err)
		return 1
	}
	return 0
}

func bootstrapOwner(cfg *config.Config, handle string) error {
	if !cfg.IsOwnerHandle(handle) {
		return fmt.Errorf("%s is not in owners.handles", handle)
	}
	pw := strings.TrimSpace(os.Getenv("EGGBOT_BOOTSTRAP_PASSWORD"))
	if pw == "" {
		fmt.Fprint(os.Stderr, "bootstrap password: ")
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil {
			return err
		}
		pw = strings.TrimSpace(line)
	}
	if err := userfile.ValidatePassword(pw); err != nil {
		return err
	}
	st, err := store.Open(cfg.Store.Path)
	if err != nil {
		return err
	}
	defer st.Close()
	users := userfile.New(st, cfg.Owners.Handles)
	if err := users.SeedOwners(); err != nil {
		return err
	}
	return users.SetPass(handle, pw)
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

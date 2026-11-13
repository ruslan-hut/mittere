package main

import (
	"context"
	"flag"
	"log/slog"
	"mittere/impl/core"
	"mittere/impl/telegram"
	"mittere/internal/config"
	"mittere/internal/database"
	"mittere/internal/http-server/api"
	"mittere/internal/lib/logger"
	"mittere/internal/lib/sl"
	"time"
)

func main() {

	configPath := flag.String("conf", "config.yml", "path to config file")
	logPath := flag.String("log", "/var/log/mittere", "path to log file directory")
	flag.Parse()

	conf := config.MustLoad(*configPath)
	lg := logger.SetupLogger(conf.Env, *logPath)

	lg.Info("starting mittere", slog.String("config", *configPath), slog.String("env", conf.Env))
	lg.Debug("debug messages enabled")

	mongo, err := database.NewMongoClient(conf)
	if err != nil {
		// a relay that cannot reach its database can neither route nor
		// journal anything, so it does not pretend to work
		lg.Error("mongo client", sl.Err(err))
		return
	}
	if mongo != nil {
		defer func() {
			if err := mongo.Close(); err != nil {
				lg.Error("mongo disconnect", sl.Err(err))
			}
		}()
		lg.Info("mongo client initialized",
			slog.String("host", conf.Mongo.Host),
			slog.String("port", conf.Mongo.Port),
			slog.String("user", conf.Mongo.User),
			slog.String("database", conf.Mongo.Database),
		)
		if err = prepareDatabase(mongo, lg); err != nil {
			lg.Error("database schema", sl.Err(err))
			return
		}
	}

	// a disabled database must leave the repository unset: handing over a nil
	// *MongoDB would make a non-nil interface holding nothing
	var repo core.Repository
	if mongo != nil {
		repo = mongo
	}
	handler := core.New(repo, lg)
	handler.SetToken(conf.AuthToken)
	handler.SetLinkCodeTTL(time.Duration(conf.Link.CodeTTLMinutes) * time.Minute)

	if conf.Telegram.Enabled {
		tg, e := telegram.New(
			conf.Telegram.ApiKey,
			conf.Telegram.RateLimit,
			time.Duration(conf.Telegram.RateWindow)*time.Second,
			lg,
		)
		if e != nil {
			lg.Error("telegram api", sl.Err(e))
			return
		}
		tg.SetCommands(handler)
		tg.Start()
		defer tg.Stop()
		lg.Info("telegram api initialized", slog.String("bot", tg.BotName()))
		handler.SetTransport(tg)
	}

	// *** blocking start with http server ***
	err = api.New(conf, lg, handler)
	if err != nil {
		lg.Error("server start", sl.Err(err))
		return
	}
	lg.Error("service stopped")
}

// prepareDatabase migrates the v1 schema and creates the v2 indexes. The
// migration runs before the indexes, because the v1 user documents share the
// subscriptions collection with the v2 topic links.
func prepareDatabase(mongo *database.MongoDB, lg *slog.Logger) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	report, err := mongo.Migrate(ctx)
	if err != nil {
		return err
	}
	if report.Applied {
		lg.Info("v1 schema migrated",
			slog.Int("users", report.Users),
			slog.Int("channels", report.Channels),
			slog.Int("topics", report.Topics),
			slog.Int("subscriptions", report.Subscriptions),
			slog.Int("senders", report.Senders),
		)
	}
	if err = mongo.Prepare(ctx); err != nil {
		return err
	}
	return mongo.EnsureDefaults()
}

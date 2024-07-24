package main

import (
	"flag"
	"log/slog"
	"mittere/impl/core"
	"mittere/internal/config"
	"mittere/internal/database"
	"mittere/internal/http-server/api"
	"mittere/internal/lib/logger"
	"mittere/internal/lib/sl"
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
		lg.Error("mongo client", sl.Err(err))
	}
	if mongo != nil {
		lg.Debug("mongo client initialized",
			slog.String("host", conf.Mongo.Host),
			slog.String("port", conf.Mongo.Port),
			slog.String("user", conf.Mongo.User),
			slog.String("database", conf.Mongo.Database),
		)
	}

	handler := core.New(lg)

	// *** blocking start with http server ***
	err = api.New(conf, lg, handler)
	if err != nil {
		lg.Error("server start", sl.Err(err))
		return
	}
	lg.Error("service stopped")
}

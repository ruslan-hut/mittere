package config

import (
	"fmt"
	"github.com/ilyakaznacheev/cleanenv"
	"log"
	"sync"
)

type Config struct {
	Env    string `yaml:"env" env-default:"local" env-required:"true"`
	Listen struct {
		BindIP string `yaml:"bind_ip" env-default:"127.0.0.1"`
		Port   string `yaml:"port" env-default:"9800"`
	} `yaml:"listen"`
	AuthToken string `yaml:"auth_token" env-default:""`
	Mongo     struct {
		Enabled bool   `yaml:"enabled" env-default:"false"`
		Host    string `yaml:"host" env-default:"127.0.0.1"`
		Port    string `yaml:"port" env-default:"27017"`
		// an empty user means a database without authentication, as a local
		// one usually is; the client only authenticates when a user is given
		User     string `yaml:"user" env-default:""`
		Password string `yaml:"password" env-default:""`
		Database string `yaml:"database" env-default:""`
	} `yaml:"mongo"`
	Telegram struct {
		Enabled bool   `yaml:"enabled" env-default:"false"`
		ApiKey  string `yaml:"api_key" env-default:""`
		// RateLimit is how many messages may go to one chat per RateWindow
		// seconds; Telegram allows about 20 a minute to a group.
		RateLimit  int `yaml:"rate_limit" env-default:"20"`
		RateWindow int `yaml:"rate_window" env-default:"60"`
	} `yaml:"telegram"`
	Events struct {
		TTLDays int `yaml:"ttl_days" env-default:"90"`
	} `yaml:"events"`
	Link struct {
		CodeTTLMinutes int `yaml:"code_ttl_minutes" env-default:"30"`
	} `yaml:"link"`
}

var instance *Config
var once sync.Once

func MustLoad(path string) *Config {
	var err error
	once.Do(func() {
		instance = &Config{}
		if err = cleanenv.ReadConfig(path, instance); err != nil {
			desc, _ := cleanenv.GetDescription(instance, nil)
			err = fmt.Errorf("%s; %s", err, desc)
			instance = nil
			log.Fatal(err)
		}
	})
	return instance
}

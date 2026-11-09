package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"
	_ "time/tzdata" // embedded zone database, so timezone works on minimal images

	"github.com/spf13/viper"
)

type Config struct {
	Host     string `mapstructure:"host"`
	Port     string `mapstructure:"port"`
	DBConn   string `mapstructure:"db_conn"`
	Username string `mapstructure:"username"` // "" = any username is accepted
	// IANA zone that decides what "today" is, e.g. Europe/Berlin
	Timezone string `mapstructure:"timezone"`

	location     *time.Location
	usernameHash [32]byte
	passwordHash [32]byte
}

// LoadConfig reads config from tracker_config.yaml in the working directory
// if present, then lets matching environment variables (e.g. PASSWORD)
// override individual keys. A missing config file is not an error, but a
// password must be configured somewhere.
func LoadConfig() (Config, error) {
	v := viper.New()

	v.SetConfigName("tracker_config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")

	v.SetDefault("host", "localhost")
	v.SetDefault("port", "8080")
	v.SetDefault("db_conn", "file:tracker.db?_journal_mode=WAL&_foreign_keys=on&_busy_timeout=5000")
	v.SetDefault("username", "")
	v.SetDefault("password", "")
	v.SetDefault("timezone", "Europe/Berlin")

	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			return Config{}, fmt.Errorf("read config: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("unmarshal config: %w", err)
	}

	password := v.GetString("password")
	if password == "" {
		return Config{}, errors.New("no password configured (set 'password' in tracker_config.yaml or PASSWORD)")
	}
	cfg.passwordHash = sha256.Sum256([]byte(password))
	cfg.usernameHash = sha256.Sum256([]byte(cfg.Username))

	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		return Config{}, fmt.Errorf("load timezone %q: %w", cfg.Timezone, err)
	}
	cfg.location = loc

	return cfg, nil
}

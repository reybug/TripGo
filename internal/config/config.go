package config

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTPAddr, DatabaseURL                                                      string
	LogLevel                                                                   slog.Level
	ShutdownTimeout, ReadTimeout, ReadHeaderTimeout, WriteTimeout, IdleTimeout time.Duration
	ConnectTimeout, QueryTimeout, MaxConnLifetime                              time.Duration
	MaxConns, MinConns                                                         int32
}

func Load() (Config, error) {
	var c Config
	required := func(name string) (string, error) {
		v := os.Getenv(name)
		if v == "" {
			return "", fmt.Errorf("%s is required", name)
		}
		return v, nil
	}
	var err error
	if c.HTTPAddr, err = required("HTTP_ADDR"); err != nil {
		return c, err
	}
	if _, _, err = net.SplitHostPort(c.HTTPAddr); err != nil {
		return c, fmt.Errorf("HTTP_ADDR: %w", err)
	}
	if c.DatabaseURL, err = required("DATABASE_URL"); err != nil {
		return c, err
	}
	level, err := required("LOG_LEVEL")
	if err != nil {
		return c, err
	}
	if err = c.LogLevel.UnmarshalText([]byte(level)); err != nil {
		return c, fmt.Errorf("LOG_LEVEL: %w", err)
	}
	durations := map[string]*time.Duration{
		"SHUTDOWN_TIMEOUT":           &c.ShutdownTimeout,
		"HTTP_READ_TIMEOUT":          &c.ReadTimeout,
		"HTTP_READ_HEADER_TIMEOUT":   &c.ReadHeaderTimeout,
		"HTTP_WRITE_TIMEOUT":         &c.WriteTimeout,
		"HTTP_IDLE_TIMEOUT":          &c.IdleTimeout,
		"DATABASE_CONNECT_TIMEOUT":   &c.ConnectTimeout,
		"DATABASE_QUERY_TIMEOUT":     &c.QueryTimeout,
		"DATABASE_MAX_CONN_LIFETIME": &c.MaxConnLifetime,
	}
	for name, dest := range durations {
		value, e := required(name)
		if e != nil {
			return c, e
		}
		*dest, e = time.ParseDuration(value)
		if e != nil || *dest <= 0 {
			return c, fmt.Errorf("%s must be a positive duration", name)
		}
	}
	for name, dest := range map[string]*int32{"DATABASE_MAX_CONNS": &c.MaxConns, "DATABASE_MIN_CONNS": &c.MinConns} {
		value, e := required(name)
		if e != nil {
			return c, e
		}
		n, e := strconv.ParseInt(value, 10, 32)
		if e != nil || n < 0 {
			return c, fmt.Errorf("%s must be a non-negative int32", name)
		}
		*dest = int32(n)
	}
	if c.MaxConns == 0 || c.MinConns > c.MaxConns {
		return c, fmt.Errorf("pool requires 0 <= DATABASE_MIN_CONNS <= DATABASE_MAX_CONNS and max > 0")
	}
	return c, nil
}

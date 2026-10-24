package config

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/time/rate"
	"gopkg.in/yaml.v3"
)

type DatabaseConfig struct {
	Driver     string `yaml:"driver"`
	Source     string `yaml:"source"`
	Reset      bool   `yaml:"reset"`
	LogQueries bool   `yaml:"log_queries"`
}

type Config struct {
	LogLevel         *slog.LevelVar `yaml:"log_level"`
	Port             int            `yaml:"port"`
	Database         DatabaseConfig `yaml:"database"`
	ErrorsInResponse bool           `yaml:"errors_in_response"`
	IPRateLimit      rate.Limit     `yaml:"ip_rate_limit"`
	BurstRateLimit   int            `yaml:"burst_rate_limit"`
	// PromoteAdmin 非空时，启动阶段把该用户名的用户提升为 admin
	PromoteAdmin string `yaml:"promote_admin"`
}

// 1. Load defaults
// 2. Load config file (lowest priority) first in the current directory, then in the home directory, then in the /etc directory
// 3. Override with environment variables
// 4. Override with command flags
func LoadConfig() (*Config, error) {
	var err error

	config := &Config{
		LogLevel: &slog.LevelVar{},
		Port:     8080,
		Database: DatabaseConfig{
			Driver:     "sqlite3",
			Source:     "./blog.db",
			Reset:      false,
			LogQueries: false,
		},
		ErrorsInResponse: false,
		IPRateLimit:      10,
		BurstRateLimit:   20,
	}

	path := checkConfigPath("config.yaml")
	if path != "" {
		config, err = loadConfigFile(config, path) // lowest priority
		if err != nil {
			return nil, fmt.Errorf("loadConfig: Error loading config file: %w", err)
		}
	}

	config, err = loadConfigEnv(config)
	if err != nil {
		return nil, fmt.Errorf("loadConfig: Error loading config environment variables: %w", err)
	}

	config = loadConfigFlags(config)

	slog.Info("Loaded config", "config", config)

	return config, nil
}

// Check if the config file exists in the current directory, the home directory, or the /etc directory
// Return the path to the config file if it exists, otherwise return an empty string
// The config file is expected to be named config.yaml for the current directory,
// .sqlite-blog-config.yaml for the home directory, and sqlite-blog-config.yaml for the /etc directory
func checkConfigPath(path string) string {
	paths := []string{
		"./" + path,
		os.Getenv("HOME") + "/.sqlite-blog-" + path,
		"/etc/sqlite-blog-" + path,
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func loadConfigFile(config *Config, path string) (*Config, error) {
	cleanPath := filepath.Clean(path)
	if strings.Contains(cleanPath, "..") {
		return nil, fmt.Errorf("invalid file path")
	}

	yamlFile, err := os.ReadFile(path)
	if err != nil {
		slog.Error("loadConfigFile: Error loading config file", "error", err)
		return config, fmt.Errorf("loadConfigFile: Error loading config file: %w", err)
	}

	err = yaml.Unmarshal(yamlFile, config)
	if err != nil {
		slog.Error("loadConfigFile: Error unmarshalling config file", "error", err)
		return config, fmt.Errorf("loadConfigFile: Error unmarshalling config file: %w", err)
	}
	return config, nil
}

func stringToLogLevel(logLevel string) slog.Level {
	switch logLevel {
	case "DEBUG":
		return slog.LevelDebug
	case "INFO":
		return slog.LevelInfo
	case "WARN":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	}
	return slog.LevelInfo
}

func loadConfigEnv(config *Config) (*Config, error) {
	var err error

	if envVal := os.Getenv("LOG_LEVEL"); envVal != "" {
		config.LogLevel.Set(stringToLogLevel(envVal))
	}

	if envVal := os.Getenv("PORT"); envVal != "" {
		config.Port, err = strconv.Atoi(envVal)
		if err != nil {
			return nil, fmt.Errorf("loadConfigEnv: Error parsing port: %w", err)
		}
	}

	if envVal := os.Getenv("DATABASE_DRIVER"); envVal != "" {
		if envVal != "sqlite3" { // && envVal != "mysql" && envVal != "postgres" {
			return nil, fmt.Errorf("loadConfigEnv: Invalid database driver: %s", envVal)
		}
		config.Database.Driver = envVal
	}

	if envVal := os.Getenv("DATABASE_SOURCE"); envVal != "" {
		config.Database.Source = envVal
	}

	if envVal := os.Getenv("DATABASE_RESET"); envVal != "" {
		config.Database.Reset, err = strconv.ParseBool(envVal)
		if err != nil {
			return nil, fmt.Errorf("loadConfigEnv: Error parsing database reset: %w", err)
		}
	}

	if envVal := os.Getenv("DATABASE_LOG_QUERIES"); envVal != "" {
		config.Database.LogQueries, err = strconv.ParseBool(envVal)
		if err != nil {
			return nil, fmt.Errorf("loadConfigEnv: Error parsing database log queries: %w", err)
		}
	}

	if envVal := os.Getenv("ERRORS_IN_RESPONSE"); envVal != "" {
		config.ErrorsInResponse, err = strconv.ParseBool(envVal)
		if err != nil {
			return nil, fmt.Errorf("loadConfigEnv: Error parsing errors in response: %w", err)
		}
	}

	if envVal := os.Getenv("IP_RATE_LIMIT"); envVal != "" {
		f, err := strconv.ParseFloat(envVal, 64)
		if err != nil {
			return nil, fmt.Errorf("loadConfigEnv: Error parsing ip rate limit: %w", err)
		}
		config.IPRateLimit = rate.Limit(f)
	}

	if envVal := os.Getenv("BURST_RATE_LIMIT"); envVal != "" {
		config.BurstRateLimit, err = strconv.Atoi(envVal)
		if err != nil {
			return nil, fmt.Errorf("loadConfigEnv: Error parsing burst rate limit: %w", err)
		}
	}

	if envVal := os.Getenv("PROMOTE_ADMIN"); envVal != "" {
		config.PromoteAdmin = envVal
	}

	return config, nil
}

func loadConfigFlags(config *Config) *Config {
	var logLevel string
	flag.StringVar(&logLevel, "log-level", "", "Log level")

	flag.IntVar(&config.Port, "port", config.Port, "Port to listen on")
	flag.StringVar(&config.Database.Driver, "database-driver", config.Database.Driver, "Database driver")
	flag.StringVar(&config.Database.Source, "database-source", config.Database.Source, "Database source")
	flag.BoolVar(&config.Database.Reset, "database-reset", config.Database.Reset, "Reset database")
	flag.BoolVar(&config.Database.LogQueries, "database-log-queries", config.Database.LogQueries, "Log database queries")
	flag.BoolVar(&config.ErrorsInResponse, "errors-in-response", config.ErrorsInResponse, "Include errors in response")
	flag.StringVar(&config.PromoteAdmin, "promote-admin", config.PromoteAdmin, "Promote the given username to the admin role on startup")

	flag.Parse()

	if logLevel != "" {
		config.LogLevel.Set(stringToLogLevel(logLevel))
	}

	return config
}

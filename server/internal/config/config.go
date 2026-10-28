package config

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Environment   string
	Mode          string
	HTTPAddr      string
	DatabaseURL   string
	Redis         RedisConfig
	JWT           JWTConfig
	CredentialKey string
	CORSOrigins   []string
	Dify          DifyConfig
	Storage       StorageConfig
}

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

type JWTConfig struct {
	Secret string
	TTL    time.Duration
}

type DifyConfig struct {
	BaseURL string
	Timeout time.Duration
}

type StorageConfig struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool
}

func Load() (Config, error) {
	redisDB, err := strconv.Atoi(env("REDIS_DB", "0"))
	if err != nil {
		return Config{}, errors.New("REDIS_DB must be an integer")
	}
	jwtTTL, err := time.ParseDuration(env("JWT_TTL", "2h"))
	if err != nil {
		return Config{}, errors.New("JWT_TTL must be a valid duration")
	}
	difyTimeout, err := time.ParseDuration(env("DIFY_TIMEOUT", "120s"))
	if err != nil {
		return Config{}, errors.New("DIFY_TIMEOUT must be a valid duration")
	}
	mode := env("APP_MODE", "single")
	if mode != "single" && mode != "multi" {
		return Config{}, errors.New("APP_MODE must be single or multi")
	}
	secret := os.Getenv("JWT_SECRET")
	if len(secret) < 32 {
		return Config{}, errors.New("JWT_SECRET must contain at least 32 characters")
	}
	credentialKey := os.Getenv("CREDENTIAL_ENCRYPTION_KEY")
	if len(credentialKey) < 32 {
		return Config{}, errors.New("CREDENTIAL_ENCRYPTION_KEY must contain at least 32 characters")
	}

	return Config{
		Environment:   env("APP_ENV", "development"),
		Mode:          mode,
		HTTPAddr:      env("HTTP_ADDR", ":8080"),
		DatabaseURL:   env("DATABASE_URL", "postgres://gin_dify:gin_dify@127.0.0.1:15432/gin_dify?sslmode=disable"),
		Redis:         RedisConfig{Addr: env("REDIS_ADDR", "127.0.0.1:16379"), Password: os.Getenv("REDIS_PASSWORD"), DB: redisDB},
		JWT:           JWTConfig{Secret: secret, TTL: jwtTTL},
		CredentialKey: credentialKey,
		CORSOrigins:   splitCSV(env("CORS_ORIGINS", "http://localhost:5777")),
		Dify:          DifyConfig{BaseURL: strings.TrimRight(env("DIFY_BASE_URL", "http://127.0.0.1:5001/v1"), "/"), Timeout: difyTimeout},
		Storage: StorageConfig{
			Endpoint:  env("S3_ENDPOINT", "http://127.0.0.1:18333"),
			AccessKey: env("S3_ACCESS_KEY", "gin-dify"),
			SecretKey: env("S3_SECRET_KEY", "gin-dify-secret"),
			Bucket:    env("S3_BUCKET", "gin-dify"),
			UseSSL:    strings.EqualFold(env("S3_USE_SSL", "false"), "true"),
		},
	}, nil
}

func env(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}
	return fallback
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

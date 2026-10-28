package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Open 创建 PostgreSQL 连接池并在返回前验证数据库可达。
func Open(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database config: %w", err)
	}
	config.MaxConns = 20
	config.MinConns = 2
	config.MaxConnLifetime = time.Hour
	config.MaxConnIdleTime = 15 * time.Minute
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

// ValidateInstallation 校验运行模式和最低数据库版本，避免不兼容进程继续启动。
func ValidateInstallation(ctx context.Context, pool *pgxpool.Pool, configuredMode string, minimumSchema int) error {
	// 单/多租户模式会改变身份和计费归属规则，旧数据库还可能缺少必要约束；
	// 因此进程配置与数据库不匹配时必须拒绝启动。
	var installedMode string
	var schemaVersion int
	err := pool.QueryRow(ctx, `SELECT mode,schema_version FROM installation_settings LIMIT 1`).Scan(&installedMode, &schemaVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read installation settings: %w", err)
	}
	if installedMode != configuredMode {
		return fmt.Errorf("configured mode %s does not match installed mode %s", configuredMode, installedMode)
	}
	if schemaVersion < minimumSchema {
		return fmt.Errorf("database schema %d is older than required schema %d", schemaVersion, minimumSchema)
	}
	return nil
}

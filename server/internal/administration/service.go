package administration

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound       = errors.New("resource not found")
	ErrProtectedMenu  = errors.New("protected menu cannot be disabled")
	ErrLastSuperAdmin = errors.New("at least one platform administrator is required")
)

type Menu struct {
	RouteName string `json:"routeName"`
	Title     string `json:"title"`
	SortOrder int    `json:"sortOrder"`
	Enabled   bool   `json:"enabled"`
	Protected bool   `json:"protected"`
}

type UpdateMenuInput struct {
	Title     string `json:"title" binding:"required,min=1,max=80"`
	SortOrder int    `json:"sortOrder" binding:"min=-1000,max=1000"`
	Enabled   bool   `json:"enabled"`
}

type PlatformUser struct {
	ID              string `json:"id"`
	Username        string `json:"username"`
	DisplayName     string `json:"displayName"`
	Status          string `json:"status"`
	IsPlatformAdmin bool   `json:"isPlatformAdmin"`
}

type UpdateAdminInput struct {
	Enabled *bool `json:"enabled" binding:"required"`
}

type Service struct{ db *pgxpool.Pool }

func NewService(db *pgxpool.Pool) *Service { return &Service{db: db} }

// Menus 返回前端已注册路由的显示配置。客户端只允许用这些字段调整已编译的路由。
func (s *Service) Menus(ctx context.Context) ([]Menu, error) {
	rows, err := s.db.Query(ctx, `
		SELECT route_name,title,sort_order,enabled,protected
		FROM admin_menu_settings ORDER BY sort_order,route_name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Menu, 0)
	for rows.Next() {
		var item Menu
		if err := rows.Scan(&item.RouteName, &item.Title, &item.SortOrder, &item.Enabled, &item.Protected); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// UpdateMenu 只更新白名单里的路由配置；受保护的入口始终可见，避免管理员锁死自己。
func (s *Service) UpdateMenu(ctx context.Context, routeName, actorID string, input UpdateMenuInput) (Menu, error) {
	input.Title = strings.TrimSpace(input.Title)
	if input.Title == "" {
		return Menu{}, errors.New("menu title is required")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Menu{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var item Menu
	err = tx.QueryRow(ctx, `
		SELECT route_name,title,sort_order,enabled,protected
		FROM admin_menu_settings WHERE route_name=$1 FOR UPDATE
	`, routeName).Scan(&item.RouteName, &item.Title, &item.SortOrder, &item.Enabled, &item.Protected)
	if errors.Is(err, pgx.ErrNoRows) {
		return Menu{}, ErrNotFound
	}
	if err != nil {
		return Menu{}, err
	}
	if item.Protected && !input.Enabled {
		return Menu{}, ErrProtectedMenu
	}
	item.Title, item.SortOrder, item.Enabled = input.Title, input.SortOrder, input.Enabled
	_, err = tx.Exec(ctx, `
		UPDATE admin_menu_settings SET title=$2,sort_order=$3,enabled=$4,updated_at=NOW()
		WHERE route_name=$1
	`, item.RouteName, item.Title, item.SortOrder, item.Enabled)
	if err != nil {
		return Menu{}, err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO audit_logs (id,actor_user_id,action,resource_type,resource_id,metadata)
		VALUES (gen_random_uuid(),$1,'admin.menu.update','menu',$2,jsonb_build_object('enabled',$3,'title',$4,'sortOrder',$5))
	`, actorID, routeName, item.Enabled, item.Title, item.SortOrder)
	if err != nil {
		return Menu{}, err
	}
	return item, tx.Commit(ctx)
}

// Users 返回平台账号。只有安装级管理员路由可调用，不能暴露密码哈希。
func (s *Service) Users(ctx context.Context) ([]PlatformUser, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id::text,username,display_name,status,is_platform_admin
		FROM users ORDER BY is_platform_admin DESC,created_at,username
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]PlatformUser, 0)
	for rows.Next() {
		var item PlatformUser
		if err := rows.Scan(&item.ID, &item.Username, &item.DisplayName, &item.Status, &item.IsPlatformAdmin); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// SetPlatformAdmin 在表级写锁下检查管理员数量，防止并发撤权导致平台失去最后一位管理员。
func (s *Service) SetPlatformAdmin(ctx context.Context, targetID, actorID string, enabled bool) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return err
	}
	var current bool
	err = tx.QueryRow(ctx, `SELECT is_platform_admin FROM users WHERE id=$1 AND status='active'`, targetID).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if current == enabled {
		return nil
	}
	if !enabled {
		var adminCount int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE is_platform_admin=TRUE AND status='active'`).Scan(&adminCount); err != nil {
			return err
		}
		if adminCount <= 1 {
			return ErrLastSuperAdmin
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET is_platform_admin=$2,updated_at=NOW() WHERE id=$1`, targetID, enabled); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_logs (id,actor_user_id,action,resource_type,resource_id,metadata)
		VALUES (gen_random_uuid(),$1,'admin.user.platform_role.update','user',$2,jsonb_build_object('enabled',$3))
	`, actorID, targetID, enabled); err != nil {
		return fmt.Errorf("write role audit: %w", err)
	}
	return tx.Commit(ctx)
}

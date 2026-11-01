package administration

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"gin-dify-saas/server/internal/platform/ids"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrNotFound       = errors.New("resource not found")
	ErrProtectedMenu  = errors.New("protected menu cannot be disabled")
	ErrBuiltinMenu    = errors.New("built-in menu cannot be deleted")
	ErrInvalidMenu    = errors.New("invalid menu input")
	ErrLastSuperAdmin = errors.New("at least one platform administrator is required")
	ErrConflict       = errors.New("username already exists")
	ErrSelfDisable    = errors.New("current user cannot be disabled")
)

type Menu struct {
	RouteName string `json:"routeName"`
	Title     string `json:"title"`
	SortOrder int    `json:"sortOrder"`
	Enabled   bool   `json:"enabled"`
	Protected bool   `json:"protected"`
	MenuType  string `json:"menuType"`
	LinkURL   string `json:"linkUrl"`
	OpenMode  string `json:"openMode"`
	Icon      string `json:"icon"`
}

type UpdateMenuInput struct {
	Title     string `json:"title" binding:"required,min=1,max=80"`
	SortOrder int    `json:"sortOrder" binding:"min=-1000,max=1000"`
	Enabled   bool   `json:"enabled"`
	LinkURL   string `json:"linkUrl" binding:"omitempty,min=8,max=1024"`
	OpenMode  string `json:"openMode" binding:"omitempty,oneof=iframe window"`
	Icon      string `json:"icon" binding:"omitempty,max=80"`
}

// CreateMenuInput 新增自定义菜单入口；只允许 http(s) 外链或内嵌页面，不允许注入前端组件。
type CreateMenuInput struct {
	Title     string `json:"title" binding:"required,min=1,max=80"`
	LinkURL   string `json:"linkUrl" binding:"required,min=8,max=1024"`
	OpenMode  string `json:"openMode" binding:"required,oneof=iframe window"`
	Icon      string `json:"icon" binding:"omitempty,max=80"`
	SortOrder int    `json:"sortOrder" binding:"min=-1000,max=1000"`
	Enabled   bool   `json:"enabled"`
}

type PlatformUser struct {
	ID              string    `json:"id"`
	Username        string    `json:"username"`
	DisplayName     string    `json:"displayName"`
	Status          string    `json:"status"`
	IsPlatformAdmin bool      `json:"isPlatformAdmin"`
	CreatedAt       time.Time `json:"createdAt"`
}

type UpdateAdminInput struct {
	Enabled *bool `json:"enabled" binding:"required"`
}

// CreateUserInput 创建平台后台账号；可同时授予安装级平台管理员。
type CreateUserInput struct {
	Username        string `json:"username" binding:"required,min=3,max=64"`
	Password        string `json:"password" binding:"required,min=8,max=128"`
	DisplayName     string `json:"displayName" binding:"required,min=2,max=64"`
	IsPlatformAdmin bool   `json:"isPlatformAdmin"`
}

// UpdateUserInput 修改后台账号的基础资料、登录状态，并可选重置登录密码。
// 用户名作为稳定登录标识不允许修改，避免影响审计记录和外部引用。
type UpdateUserInput struct {
	DisplayName string `json:"displayName" binding:"required,min=2,max=64"`
	Status      string `json:"status" binding:"required,oneof=active disabled"`
	Password    string `json:"password" binding:"omitempty,min=8,max=128"`
}

type Service struct{ db *pgxpool.Pool }

func NewService(db *pgxpool.Pool) *Service { return &Service{db: db} }

// Menus 返回前端已注册路由的显示配置与管理员创建的自定义菜单入口。
func (s *Service) Menus(ctx context.Context) ([]Menu, error) {
	rows, err := s.db.Query(ctx, `
		SELECT route_name,title,sort_order,enabled,protected,menu_type,link_url,open_mode,icon
		FROM admin_menu_settings ORDER BY sort_order,route_name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Menu, 0)
	for rows.Next() {
		var item Menu
		if err := rows.Scan(&item.RouteName, &item.Title, &item.SortOrder, &item.Enabled,
			&item.Protected, &item.MenuType, &item.LinkURL, &item.OpenMode, &item.Icon); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// validateLinkURL 仅接受 http/https 完整地址，避免执行 javascript: 等危险协议。
func validateLinkURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return ErrInvalidMenu
	}
	return nil
}

// CreateMenu 新增自定义菜单入口；route_name 系统生成，不允许客户端指定，防止覆盖内置路由。
func (s *Service) CreateMenu(ctx context.Context, actorID string, input CreateMenuInput) (Menu, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.LinkURL = strings.TrimSpace(input.LinkURL)
	input.Icon = strings.TrimSpace(input.Icon)
	if input.Title == "" || validateLinkURL(input.LinkURL) != nil {
		return Menu{}, ErrInvalidMenu
	}
	id, err := ids.New()
	if err != nil {
		return Menu{}, err
	}
	routeName := "custom-" + strings.ReplaceAll(id, "-", "")[:12]
	item := Menu{
		RouteName: routeName, Title: input.Title, SortOrder: input.SortOrder,
		Enabled: input.Enabled, Protected: false, MenuType: "link",
		LinkURL: input.LinkURL, OpenMode: input.OpenMode, Icon: input.Icon,
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Menu{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `
		INSERT INTO admin_menu_settings (route_name,title,sort_order,enabled,protected,menu_type,link_url,open_mode,icon)
		VALUES ($1,$2,$3,$4,FALSE,'link',$5,$6,$7)
	`, item.RouteName, item.Title, item.SortOrder, item.Enabled, item.LinkURL, item.OpenMode, item.Icon)
	if err != nil {
		return Menu{}, err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO audit_logs (id,actor_user_id,action,resource_type,resource_id,metadata)
		VALUES (gen_random_uuid(),$1,'admin.menu.create','menu',$2,jsonb_build_object('title',$3,'linkUrl',$4,'openMode',$5))
	`, actorID, item.RouteName, item.Title, item.LinkURL, item.OpenMode)
	if err != nil {
		return Menu{}, err
	}
	return item, tx.Commit(ctx)
}

// UpdateMenu 只更新白名单里的路由配置；受保护的入口始终可见，避免管理员锁死自己。
func (s *Service) UpdateMenu(ctx context.Context, routeName, actorID string, input UpdateMenuInput) (Menu, error) {
	input.Title = strings.TrimSpace(input.Title)
	if input.Title == "" {
		return Menu{}, ErrInvalidMenu
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Menu{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var item Menu
	err = tx.QueryRow(ctx, `
		SELECT route_name,title,sort_order,enabled,protected,menu_type,link_url,open_mode,icon
		FROM admin_menu_settings WHERE route_name=$1 FOR UPDATE
	`, routeName).Scan(&item.RouteName, &item.Title, &item.SortOrder, &item.Enabled,
		&item.Protected, &item.MenuType, &item.LinkURL, &item.OpenMode, &item.Icon)
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
	if item.MenuType == "link" {
		if linkURL := strings.TrimSpace(input.LinkURL); linkURL != "" {
			if validateLinkURL(linkURL) != nil {
				return Menu{}, ErrInvalidMenu
			}
			item.LinkURL = linkURL
		}
		if input.OpenMode != "" {
			item.OpenMode = input.OpenMode
		}
		item.Icon = strings.TrimSpace(input.Icon)
	}
	_, err = tx.Exec(ctx, `
		UPDATE admin_menu_settings SET title=$2,sort_order=$3,enabled=$4,link_url=$5,open_mode=$6,icon=$7,updated_at=NOW()
		WHERE route_name=$1
	`, item.RouteName, item.Title, item.SortOrder, item.Enabled, item.LinkURL, item.OpenMode, item.Icon)
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

// DeleteMenu 仅删除管理员创建的自定义菜单；内置路由菜单与受保护入口不可删除。
func (s *Service) DeleteMenu(ctx context.Context, routeName, actorID string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var menuType string
	err = tx.QueryRow(ctx, `
		SELECT menu_type FROM admin_menu_settings WHERE route_name=$1 FOR UPDATE
	`, routeName).Scan(&menuType)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if menuType != "link" {
		return ErrBuiltinMenu
	}
	if _, err := tx.Exec(ctx, `DELETE FROM admin_menu_settings WHERE route_name=$1`, routeName); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO audit_logs (id,actor_user_id,action,resource_type,resource_id,metadata)
		VALUES (gen_random_uuid(),$1,'admin.menu.delete','menu',$2,'{}'::jsonb)
	`, actorID, routeName)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Users 返回平台账号。只有安装级管理员路由可调用，不能暴露密码哈希。
func (s *Service) Users(ctx context.Context) ([]PlatformUser, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id::text,username,display_name,status,is_platform_admin,created_at
		FROM users ORDER BY is_platform_admin DESC,created_at,username
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]PlatformUser, 0)
	for rows.Next() {
		var item PlatformUser
		if err := rows.Scan(&item.ID, &item.Username, &item.DisplayName, &item.Status, &item.IsPlatformAdmin, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// UpdateUser 更新平台账号资料和登录状态。停用超级管理员时在事务锁内校验剩余人数，
// 同时禁止当前操作者停用自己，避免误操作导致管理后台立即失去访问入口。
func (s *Service) UpdateUser(ctx context.Context, targetID, actorID string, input UpdateUserInput) (PlatformUser, error) {
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	if targetID == actorID && input.Status != "active" {
		return PlatformUser{}, ErrSelfDisable
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return PlatformUser{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return PlatformUser{}, err
	}

	var item PlatformUser
	err = tx.QueryRow(ctx, `
		SELECT id::text,username,display_name,status,is_platform_admin,created_at
		FROM users WHERE id=$1 FOR UPDATE
	`, targetID).Scan(&item.ID, &item.Username, &item.DisplayName, &item.Status, &item.IsPlatformAdmin, &item.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return PlatformUser{}, ErrNotFound
	}
	if err != nil {
		return PlatformUser{}, err
	}

	// 最后一位可登录的平台管理员不能被停用，否则安装级管理能力将无法恢复。
	if item.IsPlatformAdmin && item.Status == "active" && input.Status != "active" {
		var adminCount int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE is_platform_admin=TRUE AND status='active'`).Scan(&adminCount); err != nil {
			return PlatformUser{}, err
		}
		if adminCount <= 1 {
			return PlatformUser{}, ErrLastSuperAdmin
		}
	}

	passwordHash := ""
	if input.Password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
		if err != nil {
			return PlatformUser{}, err
		}
		passwordHash = string(hash)
	}
	if passwordHash == "" {
		_, err = tx.Exec(ctx, `
			UPDATE users SET display_name=$2,status=$3,updated_at=NOW() WHERE id=$1
		`, targetID, input.DisplayName, input.Status)
	} else {
		_, err = tx.Exec(ctx, `
			UPDATE users SET display_name=$2,status=$3,password_hash=$4,updated_at=NOW() WHERE id=$1
		`, targetID, input.DisplayName, input.Status, passwordHash)
	}
	if err != nil {
		return PlatformUser{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_logs (id,actor_user_id,action,resource_type,resource_id,metadata)
		VALUES (gen_random_uuid(),$1,'admin.user.update','user',$2,
		        jsonb_build_object('displayName',$3,'status',$4,'passwordReset',$5))
	`, actorID, targetID, input.DisplayName, input.Status, passwordHash != ""); err != nil {
		return PlatformUser{}, fmt.Errorf("write user audit: %w", err)
	}
	item.DisplayName = input.DisplayName
	item.Status = input.Status
	return item, tx.Commit(ctx)
}

// SetPlatformAdmin 在表级写锁下检查管理员数量，防止并发撤权导致平台失去最后一位管理员。
// is_platform_admin 是超管旁路标记，user_roles 中的 super_admin 绑定与其保持同步。
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
	if err := syncSuperAdminRole(ctx, tx, targetID, enabled); err != nil {
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

// CreateUser 创建平台后台账号并加入默认租户（登录与鉴权以成员关系为前提）。
// 可同时授予安装级平台管理员，授予的账号立即可管理全部安装级路由。
func (s *Service) CreateUser(ctx context.Context, actorID string, input CreateUserInput) (PlatformUser, error) {
	username := strings.ToLower(strings.TrimSpace(input.Username))
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		return PlatformUser{}, err
	}
	var defaultTenantID string
	if err := s.db.QueryRow(ctx, `SELECT default_tenant_id::text FROM installation_settings LIMIT 1`).Scan(&defaultTenantID); err != nil {
		return PlatformUser{}, err
	}
	id, _ := ids.New()
	membershipID, _ := ids.New()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return PlatformUser{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		INSERT INTO users (id, username, password_hash, display_name, status, is_platform_admin)
		VALUES ($1,$2,$3,$4,'active',$5)
	`, id, username, string(passwordHash), strings.TrimSpace(input.DisplayName), input.IsPlatformAdmin); err != nil {
		return PlatformUser{}, classifyConflict(err)
	}
	// 后台账号也需要成员关系才能通过认证中间件，默认以成员身份加入默认租户。
	if _, err := tx.Exec(ctx, `
		INSERT INTO tenant_members (id, tenant_id, user_id, role, status)
		VALUES ($1,$2,$3,'member','active')
	`, membershipID, defaultTenantID, id); err != nil {
		return PlatformUser{}, err
	}
	if input.IsPlatformAdmin {
		if err := syncSuperAdminRole(ctx, tx, id, true); err != nil {
			return PlatformUser{}, err
		}
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_logs (id,actor_user_id,action,resource_type,resource_id,metadata)
		VALUES (gen_random_uuid(),$1,'admin.user.create','user',$2,
		        jsonb_build_object('username',$3,'isPlatformAdmin',$4))
	`, actorID, id, username, input.IsPlatformAdmin); err != nil {
		return PlatformUser{}, fmt.Errorf("write user audit: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return PlatformUser{}, err
	}
	return PlatformUser{
		ID: id, Username: username, DisplayName: strings.TrimSpace(input.DisplayName),
		Status: "active", IsPlatformAdmin: input.IsPlatformAdmin, CreatedAt: time.Now(),
	}, nil
}

// syncSuperAdminRole 在调用方事务内同步 user_roles 的 super_admin 绑定。
func syncSuperAdminRole(ctx context.Context, tx pgx.Tx, userID string, enabled bool) error {
	if enabled {
		_, err := tx.Exec(ctx, `
			INSERT INTO user_roles (user_id, role_id)
			SELECT $1, id FROM roles WHERE code='super_admin' AND scope='platform' AND tenant_id IS NULL
			ON CONFLICT DO NOTHING
		`, userID)
		return err
	}
	_, err := tx.Exec(ctx, `
		DELETE FROM user_roles ur USING roles r
		WHERE ur.user_id=$1 AND ur.role_id=r.id AND r.code='super_admin' AND r.scope='platform'
	`, userID)
	return err
}

// classifyConflict 将用户名唯一约束冲突转换为领域冲突错误。
func classifyConflict(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrConflict
	}
	return err
}

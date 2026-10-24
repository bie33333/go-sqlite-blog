package users

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/AndreHeber/go-sqlite-blog/models"
)

// 角色 ID 与 tables.sql 中的 roles 种子数据保持一致。
const (
	RoleAdmin  int64 = 1
	RoleEditor int64 = 2
	RoleAuthor int64 = 3
)

// RoleName 返回角色的可读名称。
func RoleName(roleID int64) string {
	switch roleID {
	case RoleAdmin:
		return "admin"
	case RoleEditor:
		return "editor"
	case RoleAuthor:
		return "author"
	default:
		return "unknown"
	}
}

// RoleLabel 返回角色在界面上展示的中文名称。
func RoleLabel(roleID int64) string {
	switch roleID {
	case RoleAdmin:
		return "管理员"
	case RoleEditor:
		return "编辑"
	case RoleAuthor:
		return "作者"
	default:
		return "未知角色"
	}
}

// IsAdminRole 判断角色是否为管理员。
func IsAdminRole(roleID int64) bool {
	return roleID == RoleAdmin
}

// GetRoleID 从数据库读取用户的当前角色。
// 以数据库为准而不是 session，这样角色变更（提升/降级）立即生效。
func GetRoleID(env *models.Env, userID int64) (int64, error) {
	var roleID int64
	err := env.DB.QueryRowContext(env.Ctx, `SELECT role_id FROM users WHERE id = ?`, userID).Scan(&roleID)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("GetRoleID: user %d not found", userID)
	}
	if err != nil {
		env.Logger.Error("models: GetRoleID", "error", err, "user_id", userID)
		return 0, fmt.Errorf("GetRoleID: %w", err)
	}
	return roleID, nil
}

// PromoteToAdmin 把已存在的用户提升为 admin。
func PromoteToAdmin(env *models.Env, username string) error {
	return SetRoleByUsername(env, username, RoleAdmin)
}

// SetRoleByUsername 按用户名设置角色，用户名不存在时返回错误。
func SetRoleByUsername(env *models.Env, username string, roleID int64) error {
	if username == "" {
		return errors.New("SetRoleByUsername: username is required")
	}

	result, err := env.DB.ExecContext(env.Ctx, `UPDATE users SET role_id = ? WHERE username = ?`, roleID, username)
	if err != nil {
		env.Logger.Error("models: SetRoleByUsername", "error", err, "username", username, "role_id", roleID)
		return fmt.Errorf("SetRoleByUsername: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("SetRoleByUsername: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("SetRoleByUsername: user %q not found", username)
	}

	if env.LogDBQueries {
		env.Logger.Info("models: SetRoleByUsername", "username", username, "role_id", roleID)
	}
	return nil
}

package tenant

import "time"

type TenantItem struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	Role      string    `json:"role"`
	Status    string    `json:"status"`
	Current   bool      `json:"current"`
	CreatedAt time.Time `json:"createdAt"`
}

type Member struct {
	UserID      string    `json:"userId"`
	Username    string    `json:"username"`
	DisplayName string    `json:"displayName"`
	Role        string    `json:"role"`
	Status      string    `json:"status"`
	JoinedAt    time.Time `json:"joinedAt"`
}

type RegisterResult struct {
	AccessToken string `json:"accessToken"`
	TenantID    string `json:"tenantId"`
}

type SwitchResult struct {
	AccessToken string `json:"accessToken"`
	TenantID    string `json:"tenantId"`
	Role        string `json:"role"`
}

type Invitation struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenantId"`
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

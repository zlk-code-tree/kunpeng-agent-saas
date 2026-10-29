package identity

type LoginResult struct {
	AccessToken string `json:"accessToken"`
}

type UserInfo struct {
	UserID   string   `json:"userId"`
	Username string   `json:"username"`
	RealName string   `json:"realName"`
	Avatar   string   `json:"avatar"`
	Roles    []string `json:"roles"`
	Desc     string   `json:"desc"`
	HomePath string   `json:"homePath"`
	Token    string   `json:"token"`
}

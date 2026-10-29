package integration

import "time"

type Instance struct {
	ID                  string     `json:"id"`
	Name                string     `json:"name"`
	BaseURL             string     `json:"baseUrl"`
	Status              string     `json:"status"`
	DeploymentType      string     `json:"deploymentType"`
	HealthStatus        string     `json:"healthStatus"`
	LastHealthCheckAt   *time.Time `json:"lastHealthCheckAt,omitempty"`
	ConsecutiveFailures int        `json:"consecutiveFailures"`
	CredentialVersion   int        `json:"credentialVersion"`
	CreatedAt           time.Time  `json:"createdAt"`
}

type Provider struct {
	ID           string     `json:"id"`
	Code         string     `json:"code"`
	Name         string     `json:"name"`
	ProviderType string     `json:"providerType"`
	BaseURL      string     `json:"baseUrl"`
	Status       string     `json:"status"`
	Shared       bool       `json:"shared"`
	HealthStatus string     `json:"healthStatus"`
	LastHealthAt *time.Time `json:"lastHealthAt,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
}

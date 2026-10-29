package toolgateway

import "time"

type Tool struct {
	ID          string    `json:"id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Protocol    string    `json:"protocol"`
	Status      string    `json:"status"`
	Version     int       `json:"version"`
	Endpoint    string    `json:"endpoint"`
	CreatedAt   time.Time `json:"createdAt"`
}

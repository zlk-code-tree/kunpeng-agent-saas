package knowledge

import "time"

type KnowledgeBaseItem struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Visibility string    `json:"visibility"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"createdAt"`
}

type Document struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	ContentType    string    `json:"contentType"`
	SizeBytes      int64     `json:"sizeBytes"`
	IndexingStatus string    `json:"indexingStatus"`
	CreatedAt      time.Time `json:"createdAt"`
}

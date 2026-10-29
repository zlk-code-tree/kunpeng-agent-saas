package operations

import "time"

type ExportResult struct {
	ExportID   string         `json:"exportId"`
	Checksum   string         `json:"checksumSha256"`
	Manifest   map[string]any `json:"manifest"`
	ExportedAt time.Time      `json:"exportedAt"`
}

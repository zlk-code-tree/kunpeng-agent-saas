package dify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
)

func (c *Client) CreateDataset(ctx context.Context, baseURL, apiKey, name string) (string, error) {
	body, _ := json.Marshal(map[string]any{"name": name, "permission": "only_me"})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/datasets", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("create Dify dataset: %w", err)
	}
	defer resp.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("Dify dataset API returned %d: %s", resp.StatusCode, truncate(string(responseBody), 500))
	}
	var response struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return "", err
	}
	if response.ID == "" {
		return "", fmt.Errorf("Dify dataset response has no id")
	}
	return response.ID, nil
}

func (c *Client) UploadDocument(ctx context.Context, baseURL, apiKey, datasetID, filename string, reader io.Reader) (documentID, batch string, err error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	data, _ := json.Marshal(map[string]any{
		"indexing_technique": "high_quality",
		"process_rule":       map[string]any{"mode": "automatic"},
	})
	if err := writer.WriteField("data", string(data)); err != nil {
		return "", "", err
	}
	part, err := writer.CreateFormFile("file", filepath.Base(filename))
	if err != nil {
		return "", "", err
	}
	if _, err := io.Copy(part, reader); err != nil {
		return "", "", err
	}
	if err := writer.Close(); err != nil {
		return "", "", err
	}

	endpoint := fmt.Sprintf("%s/datasets/%s/document/create-by-file", strings.TrimRight(baseURL, "/"), datasetID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("upload Dify document: %w", err)
	}
	defer resp.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("Dify document API returned %d: %s", resp.StatusCode, truncate(string(responseBody), 500))
	}
	var response struct {
		Batch    string `json:"batch"`
		Document struct {
			ID string `json:"id"`
		} `json:"document"`
	}
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return "", "", err
	}
	return response.Document.ID, response.Batch, nil
}

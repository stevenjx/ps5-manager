package installer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	httpClient *http.Client
}

func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

type InstallPayload struct {
	Type     string   `json:"type"`
	Packages []string `json:"packages"`
}

// SendInstall 单次推送包 URL 至 PS5 (DPI 协议格式)
func (c *Client) SendInstall(ctx context.Context, ps5IP string, port int, pkgURL string) error {
	endpoint := fmt.Sprintf("http://%s:%d/api/install", ps5IP, port)

	payload := InstallPayload{
		Type:     "direct",
		Packages: []string{pkgURL},
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("connect to ps5 failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ps5 returned status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

package meta

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// UserAgent 所有出站请求统一标识
const UserAgent = "ps5-manager/1.0 (+games-manager)"

// maxJSONBytes 单个 JSON 响应体上限（TitleDB 除外，它走流式解析）
const maxJSONBytes = 32 << 20

// maxImageBytes 单张图片上限
const maxImageBytes = 16 << 20

// newHTTPClient 构造带超时的 HTTP 客户端
func newHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			MaxIdleConns:        32,
			MaxIdleConnsPerHost: 8,
			IdleConnTimeout:     60 * time.Second,
		},
	}
}

// getJSON 发起 GET 并解析 JSON 响应
func getJSON(ctx context.Context, c *http.Client, rawURL string, headers map[string]string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	return doJSON(c, req, headers, out)
}

// postJSON 发起 POST（body 为 JSON）并解析 JSON 响应
func postJSON(ctx context.Context, c *http.Client, rawURL string, headers map[string]string, body any, out any) error {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return doJSON(c, req, headers, out)
}

// postRaw 发起 POST，body 为原始文本（用于 IGDB 的 Apicalypse 查询语法）
func postRaw(ctx context.Context, c *http.Client, rawURL, contentType, body string, headers map[string]string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, strings.NewReader(body))
	if err != nil {
		return err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return doJSON(c, req, headers, out)
}

func doJSON(c *http.Client, req *http.Request, headers map[string]string, out any) error {
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		if v != "" {
			req.Header.Set(k, v)
		}
	}

	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxJSONBytes))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &HTTPError{
			StatusCode: resp.StatusCode,
			URL:        req.URL.String(),
			Body:       truncate(string(body), 300),
		}
	}
	if out == nil {
		return nil
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("解析 %s 响应失败: %w", req.URL.Host, err)
	}
	return nil
}

// HTTPError 表示非 2xx 响应
type HTTPError struct {
	StatusCode int
	URL        string
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d from %s: %s", e.StatusCode, e.URL, e.Body)
}

// downloadImage 下载图片并校验确为图片格式
func downloadImage(ctx context.Context, c *http.Client, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "image/*")

	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &HTTPError{StatusCode: resp.StatusCode, URL: rawURL}
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes))
	if err != nil {
		return nil, err
	}
	if !looksLikeImage(data) {
		return nil, fmt.Errorf("响应不是有效图片: %s", rawURL)
	}
	return data, nil
}

// looksLikeImage 通过 magic byte 判定 JPEG / PNG / WEBP / GIF
func looksLikeImage(data []byte) bool {
	switch {
	case len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return true // JPEG
	case len(data) >= 8 && data[0] == 0x89 && string(data[1:4]) == "PNG":
		return true
	case len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return true
	case len(data) >= 6 && string(data[0:3]) == "GIF":
		return true
	}
	return false
}

// joinQuery 安全拼接查询参数
func joinQuery(base string, params map[string]string) string {
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	q := u.Query()
	for k, v := range params {
		if strings.TrimSpace(v) != "" {
			q.Set(k, v)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

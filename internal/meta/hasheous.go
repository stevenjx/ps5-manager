package meta

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// FileHashes 一个文件的三种哈希（Hasheous / Playmatch 都需要）
type FileHashes struct {
	MD5  string `json:"mD5,omitempty"`
	SHA1 string `json:"shA1,omitempty"`
	CRC  string `json:"crc,omitempty"`
}

// Empty 是否一个哈希都没有
func (h FileHashes) Empty() bool {
	return h.MD5 == "" && h.SHA1 == "" && h.CRC == ""
}

// PlaymatchQuery 返回 Playmatch 使用的查询参数名（小写）
func (h FileHashes) PlaymatchQuery() map[string]string {
	return map[string]string{"md5": h.MD5, "sha1": h.SHA1, "crc": h.CRC}
}

// ComputeFileHashes 流式计算文件 MD5 / SHA1 / CRC32。
//
// 安全约束：**只允许对 PS3 等白名单平台调用**（见 HashingAllowed）。
// PS4 / PS5 的 PKG 体积巨大，严禁全文件哈希；Switch 容器同理。
// CRC32 输出为 8 位小写十六进制，与 RomM utils/hashing.py 的 crc32_to_hex 一致。
func ComputeFileHashes(ctx context.Context, path string) (FileHashes, error) {
	f, err := os.Open(path)
	if err != nil {
		return FileHashes{}, err
	}
	defer f.Close()

	md5h := md5.New()
	sha1h := sha1.New()
	crc := crc32.NewIEEE()

	buf := make([]byte, 1<<20)
	for {
		select {
		case <-ctx.Done():
			return FileHashes{}, ctx.Err()
		default:
		}
		n, err := f.Read(buf)
		if n > 0 {
			_, _ = md5h.Write(buf[:n])
			_, _ = sha1h.Write(buf[:n])
			_, _ = crc.Write(buf[:n])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return FileHashes{}, err
		}
	}

	return FileHashes{
		MD5:  hex.EncodeToString(md5h.Sum(nil)),
		SHA1: hex.EncodeToString(sha1h.Sum(nil)),
		CRC:  fmt.Sprintf("%08x", crc.Sum32()),
	}, nil
}

// ---------- Hasheous ----------

// hasheousResponse Hasheous /Lookup/ByHash 的响应体。
// 键名与字段结构取自 RomM backend/handler/metadata/hasheous_handler.py 的解析逻辑。
type hasheousResponse struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Metadata []struct {
		Source      string `json:"source"`
		ImmutableID any    `json:"immutableId"`
	} `json:"metadata"`
	Attributes []struct {
		AttributeName string `json:"attributeName"`
		Link          string `json:"link"`
	} `json:"attributes"`
	Signatures map[string]any `json:"signatures"`
}

// HasheousClient 免 API Key 的哈希匹配客户端
type HasheousClient struct {
	enabled bool
	baseURL string
	http    *http.Client
}

// NewHasheousClient 构造 Hasheous 客户端
func NewHasheousClient(cfg Config) *HasheousClient {
	base := strings.TrimRight(strings.TrimSpace(cfg.HasheousURL), "/")
	if base == "" {
		base = DefaultHasheousURL
	}
	return &HasheousClient{
		enabled: cfg.HasheousEnabled,
		baseURL: base,
		http:    newHTTPClient(cfg.Timeout),
	}
}

// Enabled 是否启用
func (c *HasheousClient) Enabled() bool { return c != nil && c.enabled }

// Lookup 按文件哈希查询，命中返回 Result（含 IGDB ID），未命中返回 (nil, nil)
func (c *HasheousClient) Lookup(ctx context.Context, h FileHashes) (*Result, error) {
	if !c.Enabled() || h.Empty() {
		return nil, nil
	}
	// 接口要求 POST 一个哈希对象数组
	payload := []map[string]string{}
	item := map[string]string{}
	if h.MD5 != "" {
		item["mD5"] = h.MD5
	}
	if h.SHA1 != "" {
		item["shA1"] = h.SHA1
	}
	if h.CRC != "" {
		item["crc"] = h.CRC
	}
	payload = append(payload, item)

	url := joinQuery(c.baseURL+"/Lookup/ByHash", map[string]string{
		"returnAllSources": "true",
		"returnFields":     "Signatures, Metadata, Attributes",
	})

	var resp hasheousResponse
	if err := postJSON(ctx, c.http, url, nil, payload, &resp); err != nil {
		return nil, err
	}
	if resp.ID == 0 && resp.Name == "" {
		return nil, nil
	}

	r := newResult()
	r.Source = SourceHasheous
	r.Title = strings.TrimSpace(resp.Name)
	if r.Title != "" {
		r.Sources[FieldTitle] = SourceHasheous
	}

	for _, m := range resp.Metadata {
		if !strings.EqualFold(strings.TrimSpace(m.Source), "IGDB") {
			continue
		}
		if id := numericID(m.ImmutableID); id > 0 {
			r.IGDBID = id
			break
		}
	}

	for _, a := range resp.Attributes {
		if strings.EqualFold(a.AttributeName, "Logo") && strings.TrimSpace(a.Link) != "" {
			link := a.Link
			if strings.HasPrefix(link, "/") {
				link = baseOrigin(c.baseURL) + link
			}
			r.Artwork.Cover = link
			r.Sources[FieldCover] = SourceHasheous
			break
		}
	}

	return r, nil
}

// baseOrigin 从 baseURL 提取 scheme://host
func baseOrigin(base string) string {
	i := strings.Index(base, "://")
	if i < 0 {
		return base
	}
	rest := base[i+3:]
	if slash := strings.Index(rest, "/"); slash >= 0 {
		return base[:i+3+slash]
	}
	return base
}

// numericID 兼容字符串 / 数字两种 immutableId 表示
func numericID(v any) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case int:
		return int64(t)
	case int64:
		return t
	case string:
		id, err := strconv.ParseInt(strings.TrimSpace(t), 10, 64)
		if err != nil {
			return 0
		}
		return id
	}
	return 0
}

// ---------- Playmatch ----------

// playmatchResponse Playmatch /identify/ids 的响应体。
// 字段名取自 RomM backend/handler/metadata/playmatch_handler.py。
type playmatchResponse struct {
	GameMatchType    string `json:"gameMatchType"`
	ExternalMetadata []struct {
		ProviderName string `json:"providerName"`
		ProviderID   any    `json:"providerId"`
	} `json:"externalMetadata"`
}

// PlaymatchClient 免 API Key 的哈希匹配客户端（Playmatch 限速 4 req/s）
type PlaymatchClient struct {
	enabled bool
	baseURL string
	http    *http.Client
}

// NewPlaymatchClient 构造 Playmatch 客户端
func NewPlaymatchClient(cfg Config) *PlaymatchClient {
	base := strings.TrimRight(strings.TrimSpace(cfg.PlaymatchURL), "/")
	if base == "" {
		base = DefaultPlaymatchURL
	}
	return &PlaymatchClient{
		enabled: cfg.PlaymatchEnabled,
		baseURL: base,
		http:    newHTTPClient(cfg.Timeout),
	}
}

// Enabled 是否启用
func (c *PlaymatchClient) Enabled() bool { return c != nil && c.enabled }

// Lookup 按文件哈希查询，命中返回 Result（含 IGDB ID）
func (c *PlaymatchClient) Lookup(ctx context.Context, h FileHashes) (*Result, error) {
	if !c.Enabled() || h.Empty() {
		return nil, nil
	}
	url := joinQuery(c.baseURL+"/identify/ids", h.PlaymatchQuery())

	var resp playmatchResponse
	if err := getJSON(ctx, c.http, url, nil, &resp); err != nil {
		return nil, err
	}

	r := newResult()
	r.Source = SourcePlaymatch
	found := false
	for _, m := range resp.ExternalMetadata {
		if !strings.EqualFold(strings.TrimSpace(m.ProviderName), "IGDB") {
			continue
		}
		if id := numericID(m.ProviderID); id > 0 {
			r.IGDBID = id
			found = true
			break
		}
	}
	if !found {
		return nil, nil
	}
	return r, nil
}

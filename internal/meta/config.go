package meta

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// 默认外部服务地址（与 RomM 官方默认值一致）
const (
	DefaultHasheousURL  = "https://hasheous.org/api/v1"
	DefaultPlaymatchURL = "https://playmatch.retrorealm.dev/api/v2"
	// DefaultSwitchTitleDBURL 来自 RomM backend/tasks/scheduled/update_switch_titledb.py
	DefaultSwitchTitleDBURL = "https://raw.githubusercontent.com/blawar/titledb/master/US.en.json"
	// DefaultBangumiURL 番组计划（bgm.tv）官方 API，免 Key，提供中文简介
	DefaultBangumiURL = "https://api.bgm.tv"
)

// Config 元数据刮削配置。
//
// 安全约定：所有 API Key 只从环境变量或数据库 settings 读取，
// 代码与配置文件中不存在任何硬编码凭证。
type Config struct {
	// Enabled 是否启用在线刮削（关闭后仅使用本地 gamelist.xml）
	Enabled bool

	// IGDB 凭证（Twitch 应用）
	IGDBClientID     string
	IGDBClientSecret string

	// SteamGridDB 凭证
	SteamGridDBKey string

	// 哈希匹配源开关（均免 API Key）
	HasheousEnabled  bool
	PlaymatchEnabled bool
	HasheousURL      string
	PlaymatchURL     string

	// SwitchTitleDBURLs 按顺序尝试的 TitleDB 数据源。
	// 可把中文源（CN.zh.json）放前面，让 Switch 优先拿到中文名称与简介。
	SwitchTitleDBURLs []string
	// TitleDBEnabled 是否启用 Switch TitleDB。
	// 官方 US.en.json 约 90MB，首次使用会下载并缓存 7 天，因此允许关闭。
	TitleDBEnabled bool

	// BangumiEnabled 是否启用 Bangumi（bgm.tv）中文元数据源。
	// 免 API Key，提供简体中文名 / 中文简介 / 发售日 / 开发商 / 发行商 / 评分，
	// 是无 IGDB 凭证时获取中文简介的主要途径，默认开启。
	BangumiEnabled bool

	// BangumiURL Bangumi API 基址（可覆盖，便于自建或测试）
	BangumiURL string

	// Language 语言优先级（RomM LANGUAGES 的 ISO 码）
	Language []string
	// Region 地区优先级（RomM REGION_NAME_TO_PROVIDER_SHORTCODE 的 shortcode）
	Region []string

	// Concurrency 并发刮削数
	Concurrency int
	// Timeout 单次外部请求超时
	Timeout time.Duration
	// CacheDir 本地缓存目录（TitleDB 等大文件落盘）
	CacheDir string
	// TitleDBTTL TitleDB 缓存有效期
	TitleDBTTL time.Duration
}

// DefaultConfig 返回内置默认值
func DefaultConfig() Config {
	return Config{
		Enabled:           true,
		HasheousEnabled:   true,
		PlaymatchEnabled:  true,
		TitleDBEnabled:    true,
		BangumiEnabled:    true,
		HasheousURL:       DefaultHasheousURL,
		PlaymatchURL:      DefaultPlaymatchURL,
		BangumiURL:        DefaultBangumiURL,
		SwitchTitleDBURLs: []string{DefaultSwitchTitleDBURL},
		Language:          []string{"zh", "en"},
		Region:            []string{"cn", "us", "eu", "jp", "wor"},
		Concurrency:       4,
		Timeout:           20 * time.Second,
		CacheDir:          "meta-cache",
		TitleDBTTL:        7 * 24 * time.Hour,
	}
}

// ConfigFromEnv 从环境变量构建配置（未设置的项使用默认值）。
//
// 变量名与 docker-compose.yml / .env.example 中声明的一致。
func ConfigFromEnv() Config {
	c := DefaultConfig()

	c.Enabled = envBool("METADATA_ENABLED", c.Enabled)
	c.IGDBClientID = os.Getenv("IGDB_CLIENT_ID")
	c.IGDBClientSecret = os.Getenv("IGDB_CLIENT_SECRET")
	c.SteamGridDBKey = os.Getenv("STEAMGRIDDB_API_KEY")
	c.HasheousEnabled = envBool("HASHEOUS_API_ENABLED", c.HasheousEnabled)
	c.PlaymatchEnabled = envBool("PLAYMATCH_API_ENABLED", c.PlaymatchEnabled)
	c.TitleDBEnabled = envBool("SWITCH_TITLEDB_ENABLED", c.TitleDBEnabled)
	c.BangumiEnabled = envBool("BANGUMI_ENABLED", c.BangumiEnabled)

	if v := strings.TrimSpace(os.Getenv("BANGUMI_API_URL")); v != "" {
		c.BangumiURL = strings.TrimRight(v, "/")
	}
	if v := strings.TrimSpace(os.Getenv("HASHEOUS_API_URL")); v != "" {
		c.HasheousURL = strings.TrimRight(v, "/")
	}
	if v := strings.TrimSpace(os.Getenv("PLAYMATCH_API_URL")); v != "" {
		c.PlaymatchURL = strings.TrimRight(v, "/")
	}
	if v := envList("SWITCH_TITLEDB_URLS"); len(v) > 0 {
		c.SwitchTitleDBURLs = v
	}
	if v := envList("METADATA_LANGUAGE"); len(v) > 0 {
		c.Language = lowerAll(v)
	}
	if v := envList("METADATA_REGION"); len(v) > 0 {
		c.Region = lowerAll(v)
	}
	if v := strings.TrimSpace(os.Getenv("METADATA_CONCURRENCY")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.Concurrency = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("METADATA_TIMEOUT")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			c.Timeout = d
		}
	}
	if v := strings.TrimSpace(os.Getenv("METADATA_CACHE_DIR")); v != "" {
		c.CacheDir = v
	}
	if v := strings.TrimSpace(os.Getenv("METADATA_TITLEDB_TTL")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			c.TitleDBTTL = d
		}
	}
	return c
}

// SettingsReader 提供运行时配置读取（对应 store.Store 的 settings 表）
type SettingsReader interface {
	GetSetting(key string) (string, error)
}

// settings 表中的键名
const (
	SettingIGDBClientID     = "igdb_client_id"
	SettingIGDBClientSecret = "igdb_client_secret"
	SettingSGDBAPIKey       = "sgdb_api_key"
)

// Resolve 用 settings 表补齐环境变量中缺失的凭证。
// 优先级：环境变量（部署期配置）> settings 表（界面配置）。
func (c Config) Resolve(sr SettingsReader) Config {
	if sr == nil {
		return c
	}
	fromSettings := func(key, cur string) string {
		if cur != "" {
			return cur
		}
		v, err := sr.GetSetting(key)
		if err != nil {
			return cur
		}
		return strings.TrimSpace(v)
	}
	c.IGDBClientID = fromSettings(SettingIGDBClientID, c.IGDBClientID)
	c.IGDBClientSecret = fromSettings(SettingIGDBClientSecret, c.IGDBClientSecret)
	c.SteamGridDBKey = fromSettings(SettingSGDBAPIKey, c.SteamGridDBKey)
	return c
}

// HasIGDB 是否具备 IGDB 凭证
func (c Config) HasIGDB() bool {
	return c.IGDBClientID != "" && c.IGDBClientSecret != ""
}

// HasSGDB 是否具备 SteamGridDB 凭证
func (c Config) HasSGDB() bool {
	return strings.TrimSpace(c.SteamGridDBKey) != ""
}

// CacheDirFor 返回缓存目录（若配置为相对路径则基于 baseDir 解析）
func (c Config) CacheDirFor(baseDir string) string {
	if c.CacheDir == "" {
		return filepath.Join(baseDir, "meta-cache")
	}
	if filepath.IsAbs(c.CacheDir) {
		return c.CacheDir
	}
	return filepath.Join(baseDir, c.CacheDir)
}

// Mask 返回脱敏后的凭证摘要，供界面回显（只显示末尾 4 位）
func Mask(secret string) string {
	s := strings.TrimSpace(secret)
	if s == "" {
		return ""
	}
	if len(s) <= 4 {
		return strings.Repeat("*", len(s))
	}
	return strings.Repeat("*", 8) + s[len(s)-4:]
}

// ---------- 小工具 ----------

func envBool(key string, def bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if v == "" {
		return def
	}
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

func envList(key string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func lowerAll(in []string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v = strings.ToLower(strings.TrimSpace(v)); v != "" {
			out = append(out, v)
		}
	}
	return out
}

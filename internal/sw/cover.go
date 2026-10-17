package sw

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// iconSources 按优先级排列的 Switch 图标在线源。
// 目前使用 tinfoil.media：按「本体 TitleID」返回官方方形图标（256/512/1024 可选）。
var iconSources = []string{
	"https://tinfoil.media/ti/%s/512/512",
	"https://tinfoil.media/ti/%s/256/256",
}

// bannerSources 按优先级排列的 Switch 官方横幅（16:9 key art）在线源。
//
// 说明：Switch 没有免 Key 的官方「竖版盒装封面」接口，tinfoil.media 能稳定提供的是
// 方形图标（上面）和 16:9 的宣传主视觉（下面，1024×576，实测命中率很高）。
// 因此叫 banner 而不是 cover，避免和 2:3 竖版封面混淆。
var bannerSources = []string{
	"https://tinfoil.media/ti/%s/1024/576",
	"https://tinfoil.media/ti/%s/512/288",
}

// FetchOnlineBanner 按 TitleID 在线抓取 Switch 官方 16:9 横幅图（JPEG）。
// 未命中返回 nil，由调用方回退到图标。
func FetchOnlineBanner(titleID string) []byte {
	id := strings.TrimSpace(titleID)
	if id == "" {
		return nil
	}
	id = BaseTitleID(id)
	if len(id) != 16 {
		return nil
	}

	client := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
		},
	}

	for _, tmpl := range bannerSources {
		u := fmt.Sprintf(tmpl, id)
		if data := downloadImage(client, u); len(data) > 1024 {
			return data
		}
	}
	return nil
}

// FetchOnlineIcon 按 TitleID 在线抓取 Switch 游戏图标（方形 JPEG）。
// 会自动归一化到本体 TitleID（DLC/更新的 TitleID 也指向同一张图标）。
// 未命中返回 nil，由调用方回退到占位图。
func FetchOnlineIcon(titleID string) []byte {
	id := strings.TrimSpace(titleID)
	if id == "" {
		return nil
	}
	// 归一化到本体 TitleID
	id = BaseTitleID(id)
	if len(id) != 16 {
		return nil
	}

	client := &http.Client{
		Timeout: 12 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
		},
	}

	for _, tmpl := range iconSources {
		u := fmt.Sprintf(tmpl, id)
		if data := downloadImage(client, u); len(data) > 1024 {
			return data
		}
	}
	return nil
}

func downloadImage(client *http.Client, url string) []byte {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil || len(data) <= 1024 {
		return nil
	}
	// 校验确实是图片（JPEG/PNG）
	if !(len(data) >= 2 && data[0] == 0xFF && data[1] == 0xD8) &&
		!(len(data) >= 8 && data[0] == 0x89 && data[1] == 'P') {
		return nil
	}
	return data
}

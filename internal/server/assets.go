package server

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed assets
var assetsFS embed.FS

// assetsHandler 把嵌入的 assets 目录以静态文件服务的方式暴露出去
// 路径：/static/style.css, /static/app.js 等
func (*ServerHandler) assetsHandler() http.Handler {
	sub, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		return http.NotFoundHandler()
	}
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 把 /static/xxx 映射到 assets/xxx
		r.URL.Path = strings.TrimPrefix(r.URL.Path, "/static/")
		// 防止目录穿越
		if strings.Contains(r.URL.Path, "..") {
			http.NotFound(w, r)
			return
		}

		// 缓存策略：CSS / JS 设置 30 天强缓存（文件名版本化）
		if strings.HasSuffix(r.URL.Path, ".css") || strings.HasSuffix(r.URL.Path, ".js") {
			w.Header().Set("Cache-Control", "public, max-age=2592000, immutable")
		}

		fileServer.ServeHTTP(w, r)
	})
}

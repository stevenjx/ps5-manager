package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"ps5-manager/internal/cover"
	"ps5-manager/internal/discovery"
	"ps5-manager/internal/installer"
	"ps5-manager/internal/scanner"
	"ps5-manager/internal/server"
	"ps5-manager/internal/store"
	"ps5-manager/internal/watcher"
)

func main() {
	defaultConfigDir := os.Getenv("CONFIG_DIR")
	if defaultConfigDir == "" {
		defaultConfigDir = "config"
	}
	defaultGamesDir := os.Getenv("PKG_DIR")
	if defaultGamesDir == "" {
		defaultGamesDir = "games"
	}

	port := flag.Int("port", defaultPort(), "Web server port")
	dbPath := flag.String("db", filepath.Join(defaultConfigDir, "data.db"), "Database file path")
	coverDir := flag.String("covers", filepath.Join(defaultConfigDir, "covers"), "Cover art storage directory")
	gamesDir := flag.String("games", defaultGamesDir, "Directory where PKG games are stored")
	externalIP := flag.String("ip", os.Getenv("EXTERNAL_IP"), "External/Host IP for PS5 to download PKGs from")
	subnet := flag.String("subnet", "", "Subnet CIDR for PS5 auto-discovery (e.g. 192.168.1.0/24)")
	watchInterval := flag.Duration("watch", defaultWatchInterval(), "游戏目录实时监测间隔（0 表示关闭，如 5s / 10s）")
	flag.Parse()

	// 1. 初始化数据库持久化
	st, err := store.NewStore(*dbPath, *coverDir)
	if err != nil {
		log.Fatalf("Failed to initialize store: %v", err)
	}
	defer st.Close()

	// 2. 初始化核心组件
	coverStore := cover.NewStore(*coverDir)
	sc := scanner.NewScanner(st, coverStore)
	disc := discovery.NewDiscoverer()
	inst := installer.NewClient()

	cfg := server.Config{
		Port:       *port,
		ExternalIP: *externalIP,
		CoverDir:   *coverDir,
		GamesDir:   *gamesDir,
		ConfigDir:  defaultConfigDir,
		SubnetCIDR: *subnet,
	}

	handler := server.NewServerHandler(cfg, st, sc, disc, inst)

	// 2.2 元数据刮削流水线
	//     优先级：环境变量 > settings 表 > 默认值；
	//     凭证全部来自 .env / 界面配置，代码中无任何硬编码。
	pipeline := server.BuildPipeline(st, defaultConfigDir)
	sc.SetPipeline(pipeline)
	sc.SetRootsProvider(func() []string {
		return st.GetScanDirs(*gamesDir)
	})
	handler.SetPipeline(pipeline)
	{
		pc := pipeline.Config()
		log.Printf("[Meta] 元数据刮削: enabled=%v bangumi=%v titledb=%v igdb=%v steamgriddb=%v hasheous=%v playmatch=%v",
			pc.Enabled, pc.BangumiEnabled, pc.TitleDBEnabled, pc.HasIGDB(), pc.HasSGDB(),
			pc.HasheousEnabled, pc.PlaymatchEnabled)
		log.Printf("[Meta] 语言优先级=%v 地区优先级=%v", pc.Language, pc.Region)
		if !pc.HasIGDB() && !pc.HasSGDB() {
			log.Printf("[Meta] 未配置 IGDB / SteamGridDB 凭证；中文名与中文简介由免 Key 数据源提供（gamelist.xml / Bangumi / Switch TitleDB）")
		} else if !pc.HasIGDB() {
			log.Printf("[Meta] 未配置 IGDB 凭证；中文名与中文简介由免 Key 数据源提供（gamelist.xml / Bangumi / Switch TitleDB）")
		}
	}

	// 2.5 校正历史数据的 game/dlc 分类（修正早期版本将 DLC 误标为本体的问题）
	sc.ReclassifyCategories()

	// 3. 初始扫描游戏目录（扫描完成后会自动为缺失封面的游戏抓取官方竖版封面）
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		log.Println("Performing startup library scan...")
		scanDirs := st.GetScanDirs(*gamesDir)
		sc.ScanDirectories(ctx, scanDirs)

		// 扫描完成后，为尚未刮削过的游戏补齐元数据。
		// 即使关闭了在线刮削，本地 gamelist.xml 仍会被读取（离线可用）。
		log.Println("[Meta] 开始为缺失元数据的游戏执行刮削...")
		sc.EnrichAll(ctx, nil, false)
	}()

	// 3.5 启动实时目录监测：新增/删除游戏文件后自动增量更新
	wt := watcher.New(sc, func() []string {
		return st.GetScanDirs(*gamesDir)
	}, *watchInterval)
	go wt.Run(ctx)

	// 4. 配置 HTTP 服务与路由
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", *port),
		Handler: mux,
	}

	go func() {
		log.Printf("PS5 Manager listening on http://0.0.0.0:%d", *port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server listen failed: %v", err)
		}
	}()

	// 5. 优雅关机
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)
	<-stopChan

	log.Println("Shutting down PS5 Manager...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	_ = srv.Shutdown(shutdownCtx)
	log.Println("Server stopped cleanly.")
}

// defaultPort 默认监听端口，可用环境变量 PORT 覆盖（与 Dockerfile / docker-compose 一致）。
// 命令行 -port 优先级最高。
func defaultPort() int {
	if v := strings.TrimSpace(os.Getenv("PORT")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 65535 {
			return n
		}
	}
	return 8080
}

// defaultWatchInterval 默认的实时目录监测间隔，可用环境变量 WATCH_INTERVAL 覆盖
// （例如 WATCH_INTERVAL=10s 或 0 关闭）。默认 5 秒。
func defaultWatchInterval() time.Duration {
	if v := os.Getenv("WATCH_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return 5 * time.Second
}

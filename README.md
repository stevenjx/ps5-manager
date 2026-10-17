# Games Manager

![Go](https://img.shields.io/badge/Go-1.22-00ADD8?logo=go&logoColor=white)
![License](https://img.shields.io/badge/License-MIT-green)
![Platform](https://img.shields.io/badge/Platform-PS5%20%7C%20PS4%20%7C%20PS3%20%7C%20Switch-blue)

自托管的游戏库管理器，用于集中管理 **PlayStation 5 / PlayStation 4 / PlayStation 3 / Nintendo Switch** 的游戏文件：

自动扫描入库 · **免 Key 刮削中文元数据** · 抓取官方封面 · 局域网发现 PS5 · 一键把 PKG 推送到 PS5 安装。

> **不需要任何 API Key 就能拿到中文游戏名与中文简介。**
> 内置 Bangumi、Switch TitleDB、Hasheous、Playmatch 等免 Key 数据源；IGDB 与 SteamGridDB 仅作可选增强。

---

## 目录

- [功能特性](#功能特性)
- [界面](#界面)
- [快速开始](#快速开始)
- [目录与文件命名规范](#目录与文件命名规范)
- [元数据刮削](#元数据刮削)
- [配置项](#配置项)
- [HTTP API](#http-api)
- [开发](#开发)
- [常见问题](#常见问题)
- [许可证与第三方声明](#许可证与第三方声明)

---

## 功能特性

### 游戏库管理

- 支持 **PS5 / PS4 / PS3 / Switch** 四个平台，自动识别 `.pkg` / `.iso` / `.nsp` / `.xci` / `.nsz`
- 按「游戏文件夹」自动归组：识别游戏本体、Update 补丁与 DLC，卡片上显示 DLC 数量
- 目录实时监测：新增或删除文件后自动增量更新（间隔可配，可关闭）
- 从文件名解析地区 / 语言 / 版本 / 修订号 / Switch TitleID / PS 序列号

### 元数据刮削

- **中文优先**的字段回退流水线（`gamelist → Bangumi → TitleDB → 哈希 → IGDB → SteamGridDB`）
- 内置**繁简转换**（OpenCC 词表，3221 字），港版 / 台版繁体名可自动匹配简体中文数据源
- 按覆盖率打分的相似度匹配，避免「包含即命中」造成的长尾误配
- 每个字段独立记录来源，缺失时平滑回退，**任何单个数据源失败都不会中断扫描**

### 封面与素材

- PS4 / PS5：自动抓取 PS Store 官方竖版封面
- Switch：**封面与图标分开获取**——封面优先 SteamGridDB 的 2:3 竖版图（配置 Key 时），
  否则用 tinfoil.media 的官方 16:9 主视觉（免 Key，命中率高）；图标用官方方形图标。
  三类图片分目录缓存（`covers/switch-banners`、`covers/switch-icons`），互不覆盖
- 可选 SteamGridDB 补齐高清封面 / Hero 背景 / 透明 Logo / 横版 Banner
- 右键菜单支持手动上传封面、在线重取、删除封面

### 传送到 PS5

- UDP 广播自动发现局域网内的 PS5（默认 12800 端口）
- 一键把 PKG 的下载地址推送给 PS5，由其自行拉取安装（支持 Base + DLC 批量入队）

---

## 界面

启动后浏览器访问 `http://<主机IP>:8080`（Docker 部署默认映射 `8280`）。

- **平台选项卡**：PS5 / PS4 / PS3 / Switch 一键切换
- **视图切换**：封面（2:3 竖版）或图标（方形）两种卡片样式；Switch 同样支持两种模式
  （封面模式展示官方 16:9 主视觉，图标模式展示方形图标）
- **卡片操作**：列表卡片不再放「传送到 PS5」按钮（避免误触），安装入口在游戏详情里；
  Switch 保留「下载到 Switch」
- **卡片信息**：中文名 + 平台徽标 + TitleID + 年份 · 开发商 · 评分 + 容量 / 地区 / 版本
- **游戏详情弹窗**：Hero 背景图、中文名与官方名、发售日 / 开发 / 发行 / 评分 / 容量、类型标签、完整中文简介、官方截图
- **DLC 入口**：卡片右上角 `DLC (n)` 徽标与封面右下角「含 n DLC」角标；点击封面本身始终打开游戏简介
- **元数据弹窗**：数据源开关、可选 API Key、语言 / 地区优先级、批量刮削与进度显示
- **目录设置**：可视化配置扫描目录、自定义路径

---

## TV 大屏模式（手柄驱动）

除桌面端管理界面外，另有一套专为 **1080P / 4K 电视 + 手柄** 设计的全屏界面，交互与视觉移植自开源项目 [Overdrive](https://github.com/half144/overdrive)：

```
http://<主机IP>:8080/tv
```

- **入口**：桌面管理界面右上角的 **「▶ TV 大屏模式」** 按钮，或直接访问 `/tv`
- **顶栏**：搜索胶囊 + `L1` + 发现 / 游戏库 · 中间 Logo 与版本 · 高分推荐 / 最新入库 + 收藏 / 设置 / 管理界面 + `R1`
  （结构按 Overdrive Header 还原：96px 高、左右 48px 内边距）
- **Discover**：1410×660 的 16:9 Spotlight + 右侧 390px 竖版列表（3:4 封面），7 秒自动轮播、
  焦点落在列表项时暂停、当前项带进度条；下方「高分推荐 / 最新入库 / 平台专区」滑轨，
  标题右侧带圆形翻页箭头
- **Browse**：6 列网格（1920 下卡片 290px 宽），标题旁显示结果数，「筛选 △ / 排序」胶囊 +
  已选条件芯片可点击清除；排序支持 评分 / 最新 / 容量 / 名称
- **卡片**：3:4 封面 + 左下平台徽章 + 类型 / 标题 / 评分徽章 / 容量（对齐 Overdrive detailed 卡片）
- **搜索**：顶栏搜索胶囊打开覆盖层，支持名称 / 类型 / TitleID 实时过滤（`Esc` 关闭）
- **收藏**：□ 随时收藏/取消，顶栏心形带数量，独立「收藏」页集中查看
- **Detail**：Hero 大图、中文名、发售 / 开发 / 发行 / 评分 / 容量、官方截图条、完整中文简介、包清单与安装按钮
- **焦点视觉**：纯白发光描边 + 卡片上浮微缩放 + 旋转高光，远距离可辨
- **音效**：焦点移动 / 确认 / 返回 / 越界均有独立音效（取自 Overdrive 音效素材）
- **无横版素材时**：左侧用竖版封面做模糊填充，右侧立一张竖版海报（带渐隐），避免硬裁导致画面破碎
- **批量补 Hero 图**：桌面端「元数据 → TV 大屏素材 → 补全 Hero 背景图」，按游戏名从 SteamGridDB
  取 16:9 横版图并写回 `art_hero`（只补图片，不改文字字段；限速约 3 张/秒）
- **平台范围**：TV 端**只展示 PS5 / PS4**。PS3 与 Switch 的 ROM 体积、封面比例、元数据来源都不同，
  混在同一块大屏里会破坏列表一致性与手柄浏览体验，因此在后端 `tvPlatforms` 白名单统一过滤——
  catalog、Spotlight、详情深链、安装接口都不会出现这两个平台（桌面管理界面不受影响，仍显示全部）
- **平台筛选**：芯片按实际库动态生成并显示数量，Discover 为每个平台各出一条专区滑轨

### 按键映射

| 手柄 | 键盘 | 功能 |
|---|---|---|
| ✕ Cross | `Enter` / `Space` | 确认 / 进入详情 / 开始安装 |
| ○ Circle | `Esc` / `Backspace` | 返回上一层 / 关闭抽屉 |
| △ Triangle | `T` | 打开筛选抽屉 |
| □ Square | `F` | 收藏 / 取消收藏 |
| L1 / R1 | `Q` / `E` | 在「发现 / 游戏库 / 收藏」间循环 |
| 方向键 / 左摇杆 | 方向键 / `WASD` | 焦点移动（长按加速：380ms 后每 110ms 一步） |
| — | `M` | 静音 |

> 手柄通过浏览器 **Gamepad API** 接入，即插即用；PS5 / Xbox 手柄在 Chrome、Edge 下均为标准映射。

### 安装流程

在详情页按 ✕ 即可把选中的包下发给 PS5：服务端会生成局域网直链并调用 `internal/installer` 异步推送，卡片右下角显示实时进度条。

首次使用若未配置 PS5 地址，前端会自动调用局域网发现（`/api/v1/devices`）取第一台设备；成功一次后会记住该地址（`tv_ps5_ip`）。

---

## 快速开始

### 方式一：Docker Compose（推荐，适合 NAS）

```bash
git clone https://github.com/stevenjx/ps5-manager.git
cd ps5-manager

# 按需修改 docker-compose.yml 中的挂载路径与 EXTERNAL_IP
vim docker-compose.yml

docker compose up -d
```

> **重要**：容器使用 `network_mode: host`，这是为了让局域网设备发现（UDP 广播）正常工作，
> 同时避免桥接转发带来的吞吐损耗。因此 `PORT` 就是宿主机端口。

### 方式二：使用预构建镜像

CI 会自动构建多架构镜像并推送到 GitHub Container Registry：

```bash
docker pull ghcr.io/stevenjx/ps5-manager:latest

docker run -d \
  --name ps5-manager \
  --network host \
  -e PORT=8280 \
  -e CONFIG_DIR=/config \
  -e PKG_DIR=/packages \
  -e EXTERNAL_IP=192.168.1.10 \
  -v ./config:/config \
  -v /volume1:/packages:ro \
  ghcr.io/stevenjx/ps5-manager:latest
```

### 方式三：本地构建

需要 **Go 1.22+** 与 **C 编译器**（因为使用了 `mattn/go-sqlite3`，需要 CGO）：

```bash
# macOS
brew install go gcc

# Debian / Ubuntu
sudo apt install golang gcc libsqlite3-dev

go build -o ps5-manager ./cmd/server
./ps5-manager -games /path/to/games
```

---

## 目录与文件命名规范

推荐使用统一的库结构，扫描器会自动识别平台与分类：

```
/packages
├── ps5/
│   └── God of War (2018) (USA) [PPSA07408]/
│       ├── PPSA07408 - God of War.pkg
│       ├── update/
│       │   └── God of War [v1.33].pkg
│       └── dlc/
│           └── God of War - Valhalla [DLC].pkg
├── ps4/
│   └── Bloodborne (Europe) [CUSA00207]/
│       └── CUSA00207 - Bloodborne.pkg
├── ps3/
│   └── Demon's Souls (Europe) (En,Fr,De) [BLES00932].iso
├── switch/
│   └── The Legend of Zelda [01007EF00011E000]/
│       ├── The Legend of Zelda [01007EF00011E000][v0].nsp
│       └── The Legend of Zelda [01007EF00011E000][v65536][UPD].nsp
└── gamelist.xml          # 可选：为整个库提供中文名与中文简介
```

**文件名标签**（可任意组合，顺序不限）：

| 标签 | 示例 | 说明 |
|---|---|---|
| 地区 | `(USA)` `(Europe)` `(Japan)` `(Asia)` | 支持全称与 GoodTools 短码 |
| 语言 | `(En,Fr,De,Ja)` | 逗号分隔 |
| 版本 | `[v0]` `[v65536]` `[v1.33]` | Switch 版本号 / Update 标识 |
| 修订 | `(Rev 1)` `(Rev A)` | |
| PS 序列号 | `[BLES00932]` `[CUSA00207]` `[PPSA07408]` | 自动识别，PS3 可用于哈希匹配 |
| Switch TitleID | `[01007EF00011E000]` | 16 位十六进制 |
| 数据源 ID | `(igdb-1234)` | 内联标签，命中后**直接按 ID 精确获取**，跳过名称搜索 |

完整的标签解析规则见 [`games-manager/NAMING-CONVENTIONS.md`](games-manager/NAMING-CONVENTIONS.md)（解析顺序严格照搬 RomM 5.4.0 的 `parse_tags` 实现）。

### 本地中文元数据（可选，优先级最高）

在游戏目录或库根目录放置 `gamelist.xml`（ES-DE / Batocera / RetroBAT 通用格式），即可注入中文名与中文简介，且**完全离线可用**：

```xml
<?xml version="1.0" encoding="UTF-8"?>
<gameList>
  <game>
    <path>./Demon's Souls (Europe) (En,Fr,De) [BLES00932].iso</path>
    <name>恶魔之魂</name>
    <desc>《恶魔之魂》是由 FromSoftware 开发的动作角色扮演游戏……</desc>
    <developer>FromSoftware</developer>
    <publisher>索尼电脑娱乐</publisher>
    <genre>动作角色扮演</genre>
    <releasedate>20090205T000000</releasedate>
    <rating>0.89</rating>
  </game>
</gameList>
```

---

## 元数据刮削

### 数据源一览

| 数据源 | 是否需要 Key | 提供内容 | 适用范围 |
|---|---|---|---|
| **本地 `gamelist.xml`** | 🟢 免 Key | 中文名、中文简介、开发商、发行商、类型、本地封面 | 全平台 |
| **Bangumi**（bgm.tv） | 🟢 免 Key | 简体中文名、中文简介、开发商、发行商、评分、封面 | 全平台 |
| **Switch TitleDB** | 🟢 免 Key | Switch 官方名称、简介、发行商、Banner、BoxArt、截图 | Switch |
| **Hasheous** | 🟢 免 Key | 镜像哈希 → IGDB ID | 仅 PS3 |
| **Playmatch** | 🟢 免 Key | 镜像哈希 → IGDB ID | 仅 PS3 |
| IGDB | ⚪ 可选增强 | 官方英文简介、发售日、开发商、发行商、竖版封面、截图 | 全平台 |
| SteamGridDB | ⚪ 可选增强 | 高清封面、Hero 背景、透明 Logo、横版 Banner（**仅图片，无文字**） | 全平台 |

> ⚠️ SteamGridDB 只提供图片素材，**不提供任何文字简介**。中文简介来自 Bangumi 或 `gamelist.xml`。

### 回退顺序（First match wins）

```
1. gamelist      本地中文（最高优先级，离线可用）
2. bangumi       中文在线（免 Key）
3. titledb       Switch 官方（按 TitleID 精确匹配）
4. hasheous / playmatch   哈希匹配 → 回填 IGDB ID（仅 PS3）
5. igdb          英文官方兜底
6. sgdb          高清素材补齐（只填空缺字段）
```

每个字段独立回退：**中文源排在英文源之前**，因此已有中文简介时不会被英文简介顶替；
只有中文源全部落空才会回退到 IGDB。封面也做了兜底处理——IGDB / SteamGridDB 都没有时才使用 Bangumi 的封面。

### 哈希策略（重要约束）

- **只有 PS3 允许全文件哈希匹配**（标准 Redump 镜像体积可控，且文件名不规范的情况多）
- **PS4 / PS5 的 PKG 与 Switch 的 NSP/XCI 严禁哈希**：这些文件动辄数十 GB，全文件读取会造成大量 IO 开销

它们只走「规范化文件名 + `(igdb-xxxx)` 语义化标签」的纯文件名匹配。

---

## 配置项

### 环境变量

| 变量 | 默认值 | 说明 |
|---|---|---|
| `PORT` | `8080` | HTTP 服务端口（命令行 `-port` 优先级更高） |
| `CONFIG_DIR` | `config` | 数据库 / 封面 / 缓存的存放根目录 |
| `PKG_DIR` | `games` | 游戏文件扫描根目录 |
| `EXTERNAL_IP` | 自动探测 | PS5 下载 PKG 时使用的本机局域网 IP |
| `WATCH_INTERVAL` | `5s` | 目录实时监测间隔，设为 `0` 关闭 |

**元数据刮削**

| 变量 | 默认值 | 说明 |
|---|---|---|
| `METADATA_ENABLED` | `true` | 在线刮削总开关（关闭后仍会读取本地 `gamelist.xml`） |
| `BANGUMI_ENABLED` | `true` | Bangumi 中文源（免 Key） |
| `HASHEOUS_API_ENABLED` | `true` | Hasheous 哈希匹配（免 Key） |
| `PLAYMATCH_API_ENABLED` | `true` | Playmatch 哈希匹配（免 Key） |
| `SWITCH_TITLEDB_ENABLED` | `true` | Switch TitleDB（**首次下载约 90MB**，缓存 7 天，可关闭） |
| `IGDB_CLIENT_ID` | 空 | 可选增强，[申请地址](https://dev.twitch.tv/console/apps) |
| `IGDB_CLIENT_SECRET` | 空 | 可选增强 |
| `STEAMGRIDDB_API_KEY` | 空 | 可选增强，[申请地址](https://www.steamgriddb.com/profile/preferences/api) |
| `METADATA_LANGUAGE` | `zh,en` | 语言优先级 |
| `METADATA_REGION` | `cn,us,eu,jp,wor` | 地区优先级 |
| `METADATA_CONCURRENCY` | `4` | 并发刮削数 |
| `METADATA_TIMEOUT` | `20s` | 单次外部请求超时 |
| `METADATA_CACHE_DIR` | `meta-cache` | 大文件缓存目录（相对 `CONFIG_DIR`） |
| `METADATA_TITLEDB_TTL` | `168h` | TitleDB 缓存有效期 |
| `BANGUMI_API_URL` | `https://api.bgm.tv` | 可替换为自建镜像 |
| `HASHEOUS_API_URL` | `https://hasheous.org/api/v1` | |
| `PLAYMATCH_API_URL` | `https://playmatch.retrorealm.dev/api/v2` | |
| `SWITCH_TITLEDB_URLS` | `.../titledb/master/US.en.json` | 逗号分隔，按顺序尝试 |

> **安全约定**：所有 API Key 只从环境变量或数据库 settings 表读取，
> 代码与配置文件中**不存在任何硬编码凭证**。密钥在界面上回显时一律脱敏。

### 命令行参数

```
-port int         Web 服务端口（默认 8080）
-db string        数据库文件路径（默认 config/data.db）
-covers string    封面存储目录（默认 config/covers）
-games string     游戏文件目录（默认 $PKG_DIR 或 games）
-ip string        PS5 下载时使用的本机 IP（默认 $EXTERNAL_IP，留空则自动探测）
-subnet string    PS5 自动发现的子网网段（如 192.168.1.0/24）
-watch duration   目录监测间隔，0 关闭（默认 5s）
```

---

## HTTP API

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/api/v1/games` | 游戏列表（含元数据与 DLC 数量） |
| `GET` | `/api/v1/games/dlcs?title_id=` | 某个游戏的 DLC 清单 |
| `GET` | `/api/v1/games/{id}/icon` | 卡片图（`?mode=cover\|icon`） |
| `GET` | `/api/v1/games/{id}/art?kind=hero\|logo\|banner\|cover\|screenshot` | 高清素材代理 |
| `GET` | `/api/v1/games/{id}/metadata` | 单条元数据 |
| `POST` | `/api/v1/games/{id}/scrape` | 重新刮削此游戏 |
| `DELETE` | `/api/v1/games/{id}` | 删除游戏记录 |
| `GET` | `/api/v1/devices` | 局域网内发现的 PS5 |
| `POST` | `/api/v1/scan` | 触发重新扫描 |
| `POST` | `/api/v1/install` | 向 PS5 推送安装 |
| `GET` | `/api/v1/install/status` | 安装进度 |
| `GET` `POST` | `/api/v1/settings` | 目录设置 |
| `GET` | `/api/v1/browse` | 目录浏览（用于可视化选路径） |
| `GET` | `/api/v1/progress` | 扫描 / 传输进度 |
| `GET` | `/api/v1/metadata/status` | 刮削能力、覆盖率与实时进度 |
| `GET` `POST` | `/api/v1/metadata/settings` | 刮削设置（密钥只写不回显明文） |
| `POST` | `/api/v1/metadata/scrape` | 批量刮削（`{"force": true}` 为全量重刮） |
| `GET` | `/download/{id}` | PKG 下载（供 PS5 拉取） |

**TV 大屏端**（字段对齐 Overdrive catalog 契约）

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/api/tv/catalog` | 全局目录：`id`(TitleID) / `title` / `category` / `rating` / `size_bytes` / `cover_url` / `backdrop_url` / `version` / `package_count` |
| `GET` | `/api/tv/spotlight` | Discover 顶部 Banner 轮播（5-8 款高评分游戏） |
| `GET` | `/api/tv/pages/{title_id}` | 详情：简介、厂商、包清单（本体 / 补丁 / DLC） |
| `POST` | `/api/tv/install` | `{"title_id","package_type":"all\|base\|patch","ps5_ip"}` → 异步下发安装 |
| `GET` | `/api/tv/tasks` | 安装任务列表（卡片进度条轮询） |
| `POST` | `/api/tv/artwork/heroes` | 批量补 Hero 背景图 `{"force": false}` |
| `GET` | `/api/tv/artwork/status` | 补图任务进度 |
| `GET` | `/tv` | TV 端单页应用（支持 `#browse`、`#favorites`、`#game/<title_id>` 深链） |

桌面端支持深链，便于排查与分享：`?platform=PS5`、`?game=12`、`?meta=1`。

---

## 开发

```bash
# 运行测试
go test ./... -count=1

# 静态检查
go vet ./...

# 格式化
gofmt -w internal/ cmd/
```

### 项目结构

```
cmd/server/            程序入口（CLI 参数、装配、优雅关机）
internal/
├── naming/            文件名与库结构解析（照搬 RomM parse_tags 判定顺序）
├── meta/              元数据刮削子系统
│   ├── config.go      配置（env / settings / 默认值三级）
│   ├── pipeline.go    回退流水线（first match wins）
│   ├── gamelist.go    本地 gamelist.xml
│   ├── bangumi.go     Bangumi 中文源 + 覆盖率打分
│   ├── tradsimp.go    繁→简映射（OpenCC 子集，自动生成）
│   ├── titledb.go     Switch TitleDB（流式解析 90MB）
│   ├── hasheous.go    哈希匹配
│   ├── playmatch.go   哈希匹配
│   ├── igdb.go        IGDB 官方元数据
│   └── sgdb.go        SteamGridDB 素材
├── scanner/           扫描、入库、刮削调度、封面抓取
├── store/             SQLite 持久化 + schema 自动迁移
├── pkg/               PKG 解析与官方封面抓取
├── sw/                Switch NSP / XCI 解析
├── cover/             封面文件管理
├── discovery/         PS5 局域网发现
├── installer/         向 PS5 推送安装任务
├── watcher/           目录实时监测
└── server/            HTTP 路由、处理器与内嵌前端（web.go / assets/）
```

---

## 常见问题

**Q：需要申请 API Key 吗？**
不需要。Bangumi、Switch TitleDB、Hasheous、Playmatch、本地 `gamelist.xml` 全部免 Key。
IGDB 与 SteamGridDB 只用于补充高清素材，配置与否都不影响中文简介。

**Q：为什么 Switch 游戏的简介是英文的？**
Switch 走 TitleDB 官方数据源（英文）。部分日系游戏在 Bangumi 的条目名是日文，
英文文件名无法跨语言匹配。可在游戏目录放置 `gamelist.xml` 提供中文简介。

**Q：为什么 PS3 没有自动封面？**
PS3 没有公开的官方封面接口（PS Store 那个接口只覆盖 PS4/PS5）。
可在 `gamelist.xml` 里用 `<image>./cover.jpg</image>` 指定本地图片，或配置 IGDB / SteamGridDB。

**Q：Switch TitleDB 首次为什么要下载 90MB？**
它包含全部美版 Switch 游戏的官方名称、简介、发行商与素材。
如果不需要，把 `SWITCH_TITLEDB_ENABLED` 设为 `false` 即可，其它功能不受影响。

**Q：`EXTERNAL_IP` 填什么？**
填运行本服务那台机器的**局域网 IP**（如 `192.168.1.10`）。
PS5 会从这个地址拉取 PKG，所以必须是 PS5 能访问到的地址。

---

## 许可证与第三方声明

本项目基于 [MIT License](LICENSE) 开源。

### 第三方数据

| 来源 | 用途 | 许可 |
|---|---|---|
| [OpenCC](https://github.com/BYVoid/OpenCC) `TSCharacters.txt` | 繁→简单字映射表（生成 `internal/meta/tradsimp.go`） | Apache-2.0 |
| [blawar/titledb](https://github.com/blawar/titledb) | Switch 官方 TitleDB 数据 | 见上游仓库 |
| [Bangumi](https://bgm.tv) | 中文元数据 | 见官网 API 条款 |
| [IGDB](https://www.igdb.com) | 官方游戏元数据 | 见 Twitch 开发者条款 |
| [SteamGridDB](https://www.steamgriddb.com) | 高清图片素材 | 见官网条款 |

### 免责声明

本项目仅用于**管理用户自有的游戏文件**，不提供、不托管、不分发任何游戏本体或破解内容。
所有元数据与图片版权归各自权利人所有。

# 目录与命名规范清单

> 适用：Games Manager（RomM **5.4.0**）
> 目标平台：**PS3 / PS4 / PS5 / Nintendo Switch**
> 所有目录名、子目录名、标签语法均取自 RomM 官方 schema 与文档，未做猜测：
> - 平台 slug：`backend/utils/platform_slugs.py`（`ps3` / `ps4` / `ps5` / `switch`）
> - 分类子目录：`docs.romm.app` → Folder Structure → Multi-file games
> - 文件名标签：`docs.romm.app` → Metadata Providers → Metadata Tags in Filenames
> - Switch TitleID：`backend/utils/switch.py`

---

## 1. 平台目录映射

| 平台 | RomM slug | 库内相对路径 | 推荐组织方式 | 识别依据 |
|------|-----------|--------------|--------------|----------|
| PlayStation 3 | `ps3` | `library/roms/ps3/` | 单文件 `.iso`；多碟用游戏文件夹 | 读取镜像内 Title ID（sigil） |
| PlayStation 4 | `ps4` | `library/roms/ps4/` | 游戏文件夹（本体 + `update/` + `dlc/`） | 规范化文件名匹配 |
| PlayStation 5 | `ps5` | `library/roms/ps5/` | 游戏文件夹（本体 + `update/` + `dlc/`） | 规范化文件名匹配 |
| Nintendo Switch | `switch` | `library/roms/switch/` | 游戏文件夹（本体 + `update/` + `dlc/`） | 文件名 `[TitleID]` 解析 |

平台目录名与 slug **完全一致**，因此 `config.yml` 中**无需**配置 `system.platforms`。
匹配大小写不敏感（`PS5` / `ps5` 均可），但统一用小写最省事。

---

## 2. 目录树总览

```
library/
├── roms/
│   ├── ps3/
│   │   ├── Demon's Souls (Europe) (En,Fr,De,Es,It) [BLES00932].iso     # 单文件 = 一个游戏
│   │   └── Metal Gear Solid 4 (USA) [BLUS30109]/                       # 多碟/多文件
│   │       ├── disc1.iso
│   │       └── disc2.iso
│   │
│   ├── ps4/
│   │   └── God of War (2018) (USA) [CUSA07408]/
│   │       ├── CUSA07408 - God of War.pkg                              # 本体
│   │       ├── update/
│   │       │   └── CUSA07408 - God of War [v01.33].pkg                 # 更新补丁
│   │       └── dlc/
│   │           └── CUSA07408 - God of War - DLC Pack.pkg               # 追加内容
│   │
│   ├── ps5/
│   │   └── Astro's Playroom (World) [PPSA01325]/
│   │       ├── PPSA01325 - Astro's Playroom.pkg
│   │       └── update/
│   │           └── PPSA01325 - Astro's Playroom [v01.006.000].pkg
│   │
│   └── switch/
│       └── The Legend of Zelda - Tears of the Kingdom [0100F2C0115B6000]/
│           ├── The Legend of Zelda - Tears of the Kingdom [0100F2C0115B6000][v0].nsp   # 本体
│           ├── update/
│           │   └── The Legend of Zelda - Tears of the Kingdom [0100F2C0115B6800][v65536].nsp
│           └── dlc/
│               ├── The Legend of Zelda - Tears of the Kingdom [0100F2C0115B7001][v65536].nsp
│               └── The Legend of Zelda - Tears of the Kingdom [0100F2C0115B7002][v65536].nsp
│
├── bios/
│   └── ps3/                    # 固件（可选）
└── assets/                     # 存档 / 即时存档（由 RomM 管理，勿手动放入）
```

**核心规则**：`{game}` 这一层——**放文件**表示"一个单文件游戏"，**放文件夹**表示"一个多文件游戏"。RomM 会把同一文件夹内的本体、`update/`、`dlc/` 聚合成**一张游戏卡片**。

---

## 3. RomM 认可的分类子目录

多文件游戏文件夹内，下列子目录名会被识别为标签（**单数、复数均可**）：

| 子目录名 | 含义 | 本项目用法 |
|----------|------|-----------|
| `update` | 游戏更新 / 补丁 | ✅ PS4 / PS5 / Switch 放更新 |
| `dlc` | 追加内容 | ✅ PS4 / PS5 / Switch 放 DLC |
| `patch` | 补丁文件 | 备用 |
| `hack` / `mod` | 社区修改 | 备用 |
| `translation` | 汉化 / 民间翻译 | 备用 |
| `demo` / `prototype` | 试玩 / 原型版 | 备用 |
| `manual` | 说明书 | 备用 |
| `walkthrough` | 攻略 | 备用 |
| `soundtrack` | 原声 | 备用 |
| `cheat` | 金手指 | 备用 |
| `screenshot` | 截图 | 备用 |

> 这些名称之外的自定义子目录**不会被识别**，会被当作普通文件扫描。

---

## 4. 文件名标签语法

RomM 解析文件名中的 `[...]` 或 `(...)` 标签（两种括号都支持）：

| 标签类型 | 写法 | 示例 |
|----------|------|------|
| 地区 | ISO 码或全名 | `(Europe)`、`(USA)`、`[JP]`、`(Japan)` |
| 语言 | ISO 码或全名 | `(En,Fr,De)`、`(Chinese)` |
| 版本 / 修订 | 前缀 `rev` 或 `rev-` | `(rev v1)`、`[rev-1]` |
| 自定义标签 | 前缀 `reg` 或 `reg-` | `(reg MyOwnLang)` |
| 任意标签 | 其余内容原样导入 | `[HACK]`、`[!]` |
| **强制匹配** | 内联 provider ID | `(igdb-1234)` |

**强制匹配标签完整清单**（用于非规范英文名/缩写名识别失效时兜底）：

```
(igdb-xxxx)      → IGDB          ← 本方案主力
(moby-xxxx)      → MobyGames     ← 已禁用
(ra-xxxx)        → RetroAchievements  ← 已禁用
(ssfr-xxxx)      → ScreenScraper ← 已禁用
(launchbox-xxxx) → LaunchBox     ← 已禁用
(hltb-xxxx)      → HowLongToBeat
(steam-xxxx)     → Steam
(demozoo-xxxx)   → Demozoo
(pouet-xxxx)     → Pouët
(csdb-xxxx)      → CSDb
```

> 例：`Metal Gear Solid 4 (USA) (igdb-1064).iso` —— 无视文件名差异，直接绑定 IGDB ID 1064。
> RomM **不会**自动重命名你的文件去添加这些标签，需手动维护。

---

## 5. 各平台文件名规范

### 5.1 PS3（`.iso` 标准镜像）

```
<游戏名> (<地区>) (<语言>) [<TitleID>].iso
```

- **必须**保留镜像内的 Title ID：RomM 通过 sigil 读取镜像头部获取，无需写在文件名里也能识别；
  但把 `[BLES00932]` 写进文件名可显著提升模糊匹配命中率。
- Title ID 格式：`[BCES|BLES|BLUS|BCUS|NPEB|NPUB|NPJA|NPHB]<5位数字>`。
- 多碟：建游戏文件夹，`disc1.iso` / `disc2.iso`。

示例：

```
Demon's Souls (Europe) (En,Fr,De,Es,It) [BLES00932].iso
The Last of Us (USA) (En,Fr,Es,Pt) [BCUS98174].iso
```

### 5.2 PS4 / PS5（`.pkg`）

```
<游戏名> (<地区>) [<ContentID前缀>]/
├── <ContentID前缀> - <游戏名>.pkg
├── update/
│   └── <ContentID前缀> - <游戏名> [v<版本>].pkg
└── dlc/
    └── <ContentID前缀> - <游戏名> - <DLC名>.pkg
```

- ContentID 前缀即 Title ID：PS4 形如 `CUSA07408`，PS5 形如 `PPSA01325`。
- **严禁对 `.pkg` 做全文件哈希**——RomM 5.4.0 已将 `ps4`/`ps5` 列入
  `NON_HASHABLE_PLATFORMS`，扫描时**根本不会计算哈希**，纯靠文件名匹配。
  因此文件名必须干净、可读、含 Title ID。
- 非规范英文名/缩写名匹配失败时，用 `(igdb-xxxx)` 标签强制绑定。

示例：

```
God of War (2018) (USA) [CUSA07408]/
├── CUSA07408 - God of War.pkg
├── update/CUSA07408 - God of War [v01.33].pkg
└── dlc/CUSA07408 - God of War - Digital Deluxe Upgrade.pkg
```

### 5.3 Nintendo Switch（`.nsp` / `.xci`）

```
<游戏名> [<16位TitleID>][v<版本>].nsp
```

- Title ID 为 **16 位十六进制**，写在方括号内，例如 `[0100F2C0115B6000]`。
- 版本号写在 `[v<数字>]`，例如 `[v0]`、`[v65536]`。
- 低 12 位判定内容类型（RomM 会自动归一化到本体 TitleID）：

| 低 12 位 | 类型 | 示例 |
|----------|------|------|
| `000` | 本体（Base） | `0100F2C0115B6000` |
| `800` | 更新（Update） | `0100F2C0115B6800` |
| `1000` 起递增 | DLC | `0100F2C0115B7001`、`...7002` |

- 推荐：**本体放游戏文件夹根，更新放 `update/`，DLC 放 `dlc/`**，
  这样 RomM 自动聚合成一张卡片并显示 DLC 数量。

示例：

```
The Legend of Zelda - Tears of the Kingdom [0100F2C0115B6000]/
├── The Legend of Zelda - Tears of the Kingdom [0100F2C0115B6000][v0].nsp
├── update/
│   └── The Legend of Zelda - Tears of the Kingdom [0100F2C0115B6800][v65536].nsp
└── dlc/
    └── The Legend of Zelda - Tears of the Kingdom [0100F2C0115B7001][v65536].nsp
```

> `filesystem.embed_switch_title_ids` 保持 `false`。设为 `true` 会让 RomM **重写你的文件名**
> 来嵌入 `[TITLEID][vVERSION]`，属于破坏性操作，本方案不启用。

---

## 6. 常见反例（会导致匹配失败或错误归类）

| 反例 | 问题 |
|------|------|
| `God of War.pkg` 直接丢在 `roms/ps4/` 根目录 | 缺少 Title ID 与地区标签，模糊匹配命中率低 |
| `game.pkg` / `rom.iso` / `disk1.bin` | 无任何可用信息，必然匹配失败 |
| `roms/ps4/God of War/update.pkg`（不建 `update/` 子目录） | 更新被当成独立游戏，与本体分裂成两张卡片 |
| Switch 文件名缺少 `[TitleID]` | 无法归类本体/更新/DLC，DLC 数量显示为 0 |
| 把 `.iso` 放进 `dlc/` | 分类标签与实际内容不符，UI 标签错误 |
| 中文名 + 无英文名 + 无 Title ID | 需用 `(igdb-xxxx)` 标签强制绑定 |
| 目录名用 `PlayStation 5` | 与 slug 不符，需在 `system.platforms` 中显式映射 |

---

## 7. 与数据源 / 配置的对应关系

| 规范要素 | 对应配置 | 作用 |
|----------|----------|------|
| 游戏文件夹 + `update/` / `dlc/` | `filesystem.structure`（默认模板） | 聚合本体与追加内容为一张卡片 |
| 文件名含 Title ID | `filesystem.skip_title_id_extraction: false` | 从二进制提取 Title ID（Switch / PS3） |
| 干净可读的文件名 | `scan.priority.metadata` 中 IGDB 兜底 | 规范化文件名匹配 IGDB |
| `(igdb-xxxx)` 强制标签 | `scan.priority.metadata` | 非规范名识别失效时的兜底 |
| `gamelist.xml`（可选） | `scan.priority.metadata` 首位 `gamelist` | 注入中文官方简介与中文译名 |
| 中文标题/封面优先 | `scan.priority.region` 首位 `cn` + `region_mode: prefer_config` | IGDB 本地化（zh-CN） |
| 高清封面 / 透明 Logo | `scan.priority.artwork` 首位 `sgdb` | SteamGridDB 素材 |

### 可选：本地中文元数据（`gamelist.xml`）

在游戏文件夹内放置 `gamelist.xml` 即可注入中文简介与译名（优先级最高）：

```xml
<?xml version="1.0" encoding="UTF-8"?>
<gameList>
  <game>
    <path>./God of War.pkg</path>
    <name>战神</name>
    <desc>奎托斯再度踏上北欧神话之旅……（中文官方简介）</desc>
  </game>
</gameList>
```

`<path>` 需相对**平台目录**，否则仅当同一平台内文件名唯一时才可省略。

---

## 8. ⚠️ 重要技术提示：哈希匹配对本次 4 个平台不生效

RomM 5.4.0 源码 `backend/handler/filesystem/roms_handler.py` 中：

```python
NON_HASHABLE_PLATFORMS = frozenset((
    ...
    UPS.PS3, UPS.PS4, UPS.PS5, UPS.SWITCH, UPS.SWITCH_2,
    ...
))
```

即 **PS3 / PS4 / PS5 / Switch 全部属于"不可哈希平台"**，扫描时不会计算 MD5/SHA1/CRC。
由此产生两个直接后果：

1. **Hasheous / Playmatch（哈希匹配源）对这 4 个平台不会产生匹配结果。**
   它们仍可在 `docker-compose.yml` 中保持 `true`（对未来的其它平台或 RomM 版本升级无害），
   但**不能**指望它们解决 PS3 ISO 的识别问题。
2. **识别完全依赖「二进制内 Title ID 提取 + 规范化文件名匹配」。**
   - PS3：sigil 读取镜像内 Title ID（`SIGIL_PLATFORM_SLUGS` 含 `ps3`）
   - Switch：读取容器内 TitleID（需 `prod.keys`），并支持文件名 `[TitleID]`
   - PS4 / PS5：**不在** `SIGIL_PLATFORM_SLUGS` 中，**只能靠文件名匹配**

**结论**：本方案的成败几乎完全取决于**第 5 节的命名规范**。
若遇到无法识别的游戏，唯一可靠的兜底手段是 `(igdb-xxxx)` 内联标签。

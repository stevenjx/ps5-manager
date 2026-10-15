// Package naming 实现「目录与命名规范」的解析与规范化。
//
// 取值口径与 RomM 官方 schema 完全对齐（不做任何猜测）：
//   - 平台 slug：ps3 / ps4 / ps5 / switch
//     （backend/utils/platform_slugs.py -> UPS.PS3 / PS4 / PS5 / SWITCH）
//   - 地区码：backend/handler/filesystem/base_handler.py -> REGIONS
//     / REGION_NAME_TO_PROVIDER_SHORTCODE
//   - 语言码：backend/handler/filesystem/base_handler.py -> LANGUAGES
//   - 标签判定顺序：backend/handler/filesystem/roms_handler.py -> parse_tags()
//   - 分类子目录：docs.romm.app -> Folder Structure -> Multi-file games
//   - 内联 provider 标签：docs.romm.app -> Metadata Providers -> Metadata Tags in Filenames
//
// 判定顺序（与 RomM parse_tags 逐步对应，顺序不可调换）：
//  0. 内联 provider 标签 (igdb-1234)
//  1. 地区：精确大小写匹配 REGIONS_BY_SHORTCODE      -> "U"/"J"/"NL"/"NO" 是地区
//  2. 语言：精确大小写匹配 LANGUAGES_BY_SHORTCODE    -> "De"/"Fr"/"En" 是语言
//  3. 翻译标签 [T+Eng] / [tr fr] / (Translation)
//  4. 地区：名称 / 别名 / 不同大小写的码（大小写不敏感）
//  5. 语言：名称 / 别名（大小写不敏感）
//  6. 版本 version|ver|v
//  7. 地区前缀 reg- / reg
//  8. 修订前缀 rev- / rev
//  9. 其余原样保留
//
// 关键结论（RomM issue #3026 的裁定）：
//   - "(De)" 是德语、"（Fr)" 是法语：这类「同时是地区码与语言码」的**小写**写法
//     按语言处理，因为地区码不会由 provider shortcode 表引入（de/fr/it/ru/pl/pt/fi 被排除）。
//   - 但 "nl"/"no" 例外：它们在 GoodTools 地区码表里本就存在，因此仍按地区处理。
package naming

import (
	"path/filepath"
	"regexp"
	"strings"
)

// 平台 slug（与 RomM 标准 slug 一致）
const (
	PlatformPS3    = "ps3"
	PlatformPS4    = "ps4"
	PlatformPS5    = "ps5"
	PlatformSwitch = "switch"
)

// 分类常量
const (
	CategoryGame        = "game"
	CategoryUpdate      = "update"
	CategoryDLC         = "dlc"
	CategoryPatch       = "patch"
	CategoryHack        = "hack"
	CategoryMod         = "mod"
	CategoryTranslation = "translation"
	CategoryDemo        = "demo"
	CategoryPrototype   = "prototype"
	CategoryManual      = "manual"
	CategoryWalkthrough = "walkthrough"
	CategorySoundtrack  = "soundtrack"
	CategoryCheat       = "cheat"
	CategoryScreenshot  = "screenshot"
)

// CategoryDirs 目录名（小写）-> 规范化分类。RomM 同时识别单数与复数。
var CategoryDirs = map[string]string{
	"update": CategoryUpdate, "updates": CategoryUpdate,
	"dlc": CategoryDLC, "dlcs": CategoryDLC,
	"patch": CategoryPatch, "patches": CategoryPatch,
	"hack": CategoryHack, "hacks": CategoryHack,
	"mod": CategoryMod, "mods": CategoryMod,
	"translation": CategoryTranslation, "translations": CategoryTranslation,
	"demo": CategoryDemo, "demos": CategoryDemo,
	"prototype": CategoryPrototype, "prototypes": CategoryPrototype,
	"manual": CategoryManual, "manuals": CategoryManual,
	"walkthrough": CategoryWalkthrough, "walkthroughs": CategoryWalkthrough,
	"soundtrack": CategorySoundtrack, "soundtracks": CategorySoundtrack,
	"cheat": CategoryCheat, "cheats": CategoryCheat,
	"screenshot": CategoryScreenshot, "screenshots": CategoryScreenshot,
}

// platformAliases 库内目录名（小写、已去空格）-> 平台 slug
var platformAliases = map[string]string{
	"ps3": PlatformPS3, "playstation3": PlatformPS3,
	"ps4": PlatformPS4, "playstation4": PlatformPS4,
	"ps5": PlatformPS5, "playstation5": PlatformPS5,
	"switch": PlatformSwitch, "nintendoswitch": PlatformSwitch, "ns": PlatformSwitch,
}

// regionShortcodesExact 步骤 1：精确大小写匹配的地区码。
// 直接取自 REGIONS 元组的 code 列，**大小写敏感**。
var regionShortcodesExact = map[string]string{
	"A": "au", "AS": "asi", "B": "br", "C": "ca", "CH": "cn", "E": "eu",
	"F": "fr", "FN": "fi", "G": "de", "GR": "gr", "H": "nl", "HK": "hk",
	"I": "it", "J": "jp", "K": "kr", "NL": "nl", "NO": "no", "PD": "pd",
	"R": "ru", "S": "sp", "SW": "se", "T": "tw", "U": "us", "UK": "uk",
	"UNK": "unk", "UNL": "unl", "W": "wor",
}

// languageShortcodesExact 步骤 2：精确大小写匹配的语言码。
// 直接取自 LANGUAGES 元组的 code 列，**大小写敏感**。
var languageShortcodesExact = map[string]string{
	"Ar": "ar", "Da": "da", "De": "de", "El": "el", "En": "en", "Es": "es",
	"Fi": "fi", "Fr": "fr", "It": "it", "Ja": "ja", "Ko": "ko", "Nl": "nl",
	"No": "no", "Pl": "pl", "Pt": "pt", "Ru": "ru", "Sr": "sr", "Sv": "sv",
	"Zh": "zh", "nolang": "nolang",
}

// regionAlias 步骤 4：大小写不敏感的地区别名 -> shortcode。
//
// 组成 = REGIONS 的全名 + REGIONS 的 GoodTools 码 + provider shortcode
// （其中被语言表认领的 de/fi/fr/it/nl/no/pl/pt/ru 不再引入，
//
//	但 nl/no 因已存在于 GoodTools 码表中而保留，与 RomM 行为一致）。
var regionAlias = map[string]string{
	// ---- REGIONS 全名 ----
	"australia": "au", "asia": "asi", "brazil": "br", "canada": "ca",
	"china": "cn", "england": "uk", "europe": "eu", "finland": "fi",
	"france": "fr", "germany": "de", "greece": "gr", "holland": "nl",
	"hong kong": "hk", "italy": "it", "japan": "jp", "korea": "kr",
	"netherlands": "nl", "norway": "no", "russia": "ru", "spain": "sp",
	"sweden": "se", "taiwan": "tw", "usa": "us", "world": "wor",
	"public domain": "pd", "unknown": "unk", "unlicensed": "unl",
	// ---- REGIONS GoodTools 码（小写）----
	"a": "au", "as": "asi", "b": "br", "c": "ca", "ch": "cn", "e": "eu",
	"f": "fr", "fn": "fi", "g": "de", "gr": "gr", "h": "nl", "hk": "hk",
	"i": "it", "j": "jp", "k": "kr", "nl": "nl", "no": "no", "pd": "pd",
	"r": "ru", "s": "sp", "sw": "se", "t": "tw", "u": "us", "uk": "uk",
	"unk": "unk", "unl": "unl", "w": "wor",
	// ---- provider shortcode（已排除被语言表认领者；与上面 GoodTools 码重复的
	//      gr / hk / uk 不再重复声明）----
	"au": "au", "asi": "asi", "br": "br", "ca": "ca", "cn": "cn",
	"eu": "eu", "jp": "jp", "kr": "kr", "sp": "sp",
	"se": "se", "tw": "tw", "us": "us", "wor": "wor", "ame": "ame",
	"bg": "bg", "cl": "cl", "cz": "cz", "dk": "dk", "hu": "hu", "il": "il",
	"kw": "kw", "mor": "mor", "nz": "nz", "oce": "oce", "pe": "pe",
	"sk": "sk", "tr": "tr",
	// 以下 provider shortcode 被语言表认领，按 RomM 规则**不引入**：
	//   de fi fr it nl no pl pt ru
	// 其中 nl / no 已由上面的 GoodTools 码提供，故仍按地区处理。
	// ---- REGIONS 全名（provider 专属地区）----
	"americas": "ame", "bulgaria": "bg", "chile": "cl", "czech republic": "cz",
	"denmark": "dk", "hungary": "hu", "israel": "il", "kuwait": "kw",
	"middle east": "mor", "new zealand": "nz", "oceania": "oce", "peru": "pe",
	"poland": "pl", "portugal": "pt", "slovakia": "sk", "turkey": "tr",
}

// languageAlias 步骤 5：大小写不敏感的语言别名 -> ISO 码。直接取自 LANGUAGES。
var languageAlias = map[string]string{
	"arabic": "ar", "danish": "da", "german": "de", "greek": "el",
	"english": "en", "spanish": "es", "finnish": "fi", "french": "fr",
	"italian": "it", "japanese": "ja", "korean": "ko", "dutch": "nl",
	"norwegian": "no", "polish": "pl", "portuguese": "pt", "russian": "ru",
	"serbian": "sr", "swedish": "sv", "chinese": "zh", "no language": "nolang",
	"ar": "ar", "da": "da", "de": "de", "el": "el", "en": "en", "es": "es",
	"fi": "fi", "fr": "fr", "it": "it", "ja": "ja", "ko": "ko", "nl": "nl",
	"no": "no", "pl": "pl", "pt": "pt", "ru": "ru", "sr": "sr", "sv": "sv",
	"zh": "zh", "nolang": "nolang",
}

// translationAlias 步骤 3：只有翻译标签才会用到的语言码（_TRANSLATION_LANGUAGE_ALIASES）
var translationAlias = map[string]string{
	"ara": "ar", "chi": "zh", "dan": "da", "du": "nl", "dut": "nl",
	"eng": "en", "fin": "fi", "fre": "fr", "ge": "de", "ger": "de",
	"gr": "el", "gre": "el", "ita": "it", "jap": "ja", "jp": "ja",
	"kor": "ko", "nor": "no", "pol": "pl", "por": "pt", "rus": "ru",
	"ser": "sr", "sp": "es", "spa": "es", "swe": "sv",
}

// TranslationTag 翻译标签的规范化标记（RomM TRANSLATION_TAG）
const TranslationTag = "Translation"

// providerTags 内联强制匹配标签（docs.romm.app -> Metadata Tags in Filenames）
var providerTags = map[string]bool{
	"igdb": true, "moby": true, "ra": true, "ssfr": true, "launchbox": true,
	"hltb": true, "steam": true, "demozoo": true, "pouet": true, "csdb": true,
}

var (
	// GENERIC_TAG_REGEX：(...) 与 [...]
	genericTagRe = regexp.MustCompile(`\(([^)]+)\)|\[([^\]]+)\]`)
	// VERSION_TAG_REGEX
	versionTagRe = regexp.MustCompile(`(?i)^(?:version|ver|v)(?:[\s._-](.*)|([.\d].*))$`)
	// REGION_TAG_REGEX
	regionPrefixRe = regexp.MustCompile(`(?i)^reg[\s|-](.*)$`)
	// REVISION_TAG_REGEX
	revisionPrefixRe = regexp.MustCompile(`(?i)^rev[\s|-](.*)$`)
	// TRANSLATION_TAG_REGEX（移植自 RomM，1=superseded 2=goodtools 3=patch 4=spaced 5=tosec）
	translationTagRe = regexp.MustCompile(`(?i)^(?:t(?:(-)|\+)([a-z]{2,3})(.*)|t[\s_-]+([a-z]{2,3}).*|tr(?:[\s_-]+([a-z]{2,3}))?|translat(?:ed|ion))$`)
	// 内联 provider 标签：igdb-1234
	providerTagRe = regexp.MustCompile(`^([a-z]+)-(\d+)$`)
	// Switch TitleID：16 位十六进制（RomM utils/switch.py TITLE_ID_BRACKET_REGEX）
	switchTitleIDRe = regexp.MustCompile(`\[([0-9A-Fa-f]{16})\]`)
	// PS3 Title ID / 序列号：4 位大写字母 + 5 位数字（如 BLES00932 / NPUB30123）。
	// 这是 PS3 标题库通用格式，因此无需依赖前缀白名单。
	ps3SerialRe = regexp.MustCompile(`\b([A-Z]{4}\d{5})\b`)
)

// Tag 一个文件名标签
type Tag struct {
	Kind  string // "region" | "language" | "version" | "revision" | "provider" | "other"
	Value string // 规范化值（地区=shortcode；语言=ISO 码；provider="igdb:1234"）
	Raw   string // 原始文本
}

// Parsed 文件名 / 路径的解析结果
type Parsed struct {
	Name          string   // 去掉标签后的干净游戏名
	Regions       []string // 规范化地区 shortcode（小写，去重保序）
	Languages     []string // 规范化语言 ISO 码（小写，去重保序）
	Version       string   // 版本标签值（如 "1.0" / "65536"）
	Revision      string   // 修订号（如 "v1"）
	ProviderTag   string   // 内联 provider 名（如 "igdb"）
	ProviderID    string   // 内联 provider ID（如 "1234"）
	SwitchTitleID string   // Switch 16 位 TitleID（大写）
	PSSerial      string   // PS3 Title ID / 序列号（如 BLES00932）
	Tags          []Tag    // 全部标签
}

// ProviderRef 返回形如 "igdb:1234" 的引用；无则返回空串
func (p Parsed) ProviderRef() string {
	if p.ProviderTag == "" || p.ProviderID == "" {
		return ""
	}
	return p.ProviderTag + ":" + p.ProviderID
}

// ParseFileName 解析单个文件名中的标签，判定顺序与 RomM parse_tags 一致。
func ParseFileName(name string) Parsed {
	var p Parsed
	stem := strings.TrimSuffix(name, filepath.Ext(name))

	if m := switchTitleIDRe.FindStringSubmatch(stem); len(m) == 2 {
		p.SwitchTitleID = strings.ToUpper(m[1])
	}

	// PS3 序列号（Redump / PSN 命名的常见写法）
	if m := ps3SerialRe.FindStringSubmatch(stem); len(m) == 2 {
		p.PSSerial = m[1]
	}

	for _, m := range genericTagRe.FindAllStringSubmatch(stem, -1) {
		group := m[1]
		if group == "" {
			group = m[2]
		}
		// 一个括号内可用逗号分隔多个值：(En,Fr,De)
		for _, token := range strings.Split(group, ",") {
			token = strings.TrimSpace(token)
			if token == "" {
				continue
			}
			p.consumeToken(token)
		}
	}

	p.Name = CleanName(stem)
	return p
}

// consumeToken 按 RomM parse_tags 的顺序判定单个 token。
func (p *Parsed) consumeToken(token string) {
	// 0) 内联 provider 标签（RomM 在 parse_tags 之外单独处理，这里前置更安全）
	if m := providerTagRe.FindStringSubmatch(strings.ToLower(token)); len(m) == 3 && providerTags[m[1]] {
		p.Tags = append(p.Tags, Tag{Kind: "provider", Value: m[1] + ":" + m[2], Raw: token})
		if p.ProviderTag == "" {
			p.ProviderTag, p.ProviderID = m[1], m[2]
		}
		return
	}

	// 1) 地区：精确大小写码表（"U" / "J" / "NL" / "NO"）
	if code, ok := regionShortcodesExact[token]; ok {
		p.Tags = append(p.Tags, Tag{Kind: "region", Value: code, Raw: token})
		p.Regions = appendUnique(p.Regions, code)
		return
	}

	// 2) 语言：精确大小写码表（"De" / "Fr" / "En"）
	if code, ok := languageShortcodesExact[token]; ok {
		p.Tags = append(p.Tags, Tag{Kind: "language", Value: code, Raw: token})
		p.Languages = appendUnique(p.Languages, code)
		return
	}

	// 3) 翻译标签：译文语言即玩家实际使用的语言
	if m := translationTagRe.FindStringSubmatch(token); m != nil {
		spaced, goodtools, tosec := m[4], m[2], m[5]
		code := spaced
		if code == "" {
			code = goodtools
		}
		if code == "" {
			code = tosec
		}
		lang := ""
		if code != "" {
			lang = translationLanguage(code)
		}
		bareSuperseded := m[1] != "" && m[3] == ""
		// "T Rex" 这类普通词只在码确实是语言时才认作翻译标签
		if lang != "" || (spaced == "" && !bareSuperseded) {
			p.Tags = append(p.Tags, Tag{Kind: "other", Value: TranslationTag, Raw: token})
			if lang != "" {
				p.Languages = appendUnique(p.Languages, lang)
			}
			return
		}
	}

	// 4) 地区：名称 / 别名 / 不同大小写的码（大小写不敏感）
	if code, ok := regionAlias[strings.ToLower(token)]; ok {
		p.Tags = append(p.Tags, Tag{Kind: "region", Value: code, Raw: token})
		p.Regions = appendUnique(p.Regions, code)
		return
	}

	// 5) 语言：名称 / 别名
	if code, ok := languageAlias[strings.ToLower(token)]; ok {
		p.Tags = append(p.Tags, Tag{Kind: "language", Value: code, Raw: token})
		p.Languages = appendUnique(p.Languages, code)
		return
	}

	// 6) 版本
	if m := versionTagRe.FindStringSubmatch(token); m != nil {
		v := strings.TrimSpace(m[1])
		if v == "" {
			v = strings.TrimSpace(m[2])
		}
		if v != "" {
			p.Tags = append(p.Tags, Tag{Kind: "version", Value: v, Raw: token})
			if p.Version == "" {
				p.Version = v
			}
		}
		return
	}

	// 7) 地区前缀：显式 Reg- 视为用户声明为地区，未知码也保留
	if m := regionPrefixRe.FindStringSubmatch(token); m != nil {
		raw := strings.TrimSpace(m[1])
		if raw != "" {
			code, ok := regionAlias[strings.ToLower(raw)]
			if !ok {
				code = strings.ToLower(raw)
			}
			p.Tags = append(p.Tags, Tag{Kind: "region", Value: code, Raw: token})
			p.Regions = appendUnique(p.Regions, code)
		}
		return
	}

	// 8) 修订前缀
	if m := revisionPrefixRe.FindStringSubmatch(token); m != nil {
		rev := strings.TrimSpace(m[1])
		if rev != "" {
			p.Tags = append(p.Tags, Tag{Kind: "revision", Value: rev, Raw: token})
			if p.Revision == "" {
				p.Revision = rev
			}
		}
		return
	}

	// 9) 其余原样保留
	p.Tags = append(p.Tags, Tag{Kind: "other", Value: token, Raw: token})
}

// translationLanguage 解析翻译标签所指语言（normalize_language 或翻译别名表）
func translationLanguage(code string) string {
	lower := strings.ToLower(strings.TrimSpace(code))
	if v, ok := languageAlias[lower]; ok {
		return v
	}
	return translationAlias[lower]
}

// structuralDirs 库结构自身的目录名，不是游戏文件夹
var structuralDirs = map[string]bool{
	"roms": true, "bios": true, "games": true, "library": true,
	"assets": true, "saves": true, "states": true, "screenshots": true,
}

// ParsePath 合并「文件所在目录链 + 文件名」解析。
//
// 目录链里可能出现 update/、dlc/ 等分类目录（跳过继续向上），
// 以及 roms/、平台目录等结构目录（**到此为止**，再往上就不是游戏层级了）。
// 游戏名取「从文件向上第一个非结构目录」的名称——
// 那正是 roms/{platform}/{game} 里的 {game} 层。
func ParsePath(path string) Parsed {
	fromFile := ParseFileName(filepath.Base(path))
	fileStem := fromFile.Name

	dir := filepath.Dir(path)
	gameFolder := ""
	for depth := 0; depth < 4; depth++ {
		parent := filepath.Base(dir)
		if parent == "" || parent == "." || parent == string(filepath.Separator) {
			break
		}
		lower := strings.ToLower(strings.TrimSpace(parent))

		// 结构目录（roms/、games/、平台目录…）：游戏层级到此结束，停止回溯。
		// 否则「文件直接放在平台目录」时会把库根目录名误当成游戏名。
		if DetectPlatform(parent) != "" || structuralDirs[lower] {
			break
		}
		// 分类目录（update/dlc/…）：不是游戏名，跳过但继续向上找
		if IsCategoryDir(parent) {
			next := filepath.Dir(dir)
			if next == dir {
				break
			}
			dir = next
			continue
		}

		if gameFolder == "" {
			gameFolder = parent
		}
		fromDir := ParseFileName(parent)
		fromFile.Regions = appendUnique(fromFile.Regions, fromDir.Regions...)
		fromFile.Languages = appendUnique(fromFile.Languages, fromDir.Languages...)
		if fromFile.SwitchTitleID == "" {
			fromFile.SwitchTitleID = fromDir.SwitchTitleID
		}
		if fromFile.PSSerial == "" {
			fromFile.PSSerial = fromDir.PSSerial
		}
		if fromFile.Version == "" {
			fromFile.Version = fromDir.Version
		}
		if fromFile.ProviderRef() == "" && fromDir.ProviderRef() != "" {
			fromFile.ProviderTag, fromFile.ProviderID = fromDir.ProviderTag, fromDir.ProviderID
		}

		next := filepath.Dir(dir)
		if next == dir {
			break
		}
		dir = next
	}

	// 游戏名优先取游戏文件夹名（{game} 层），文件夹缺失或本身无信息量时才退回文件名
	if gf := CleanName(gameFolder); gf != "" && !isUninformativeName(gf) {
		fromFile.Name = gf
	} else if fromFile.Name == "" {
		fromFile.Name = fileStem
	}
	return fromFile
}

// isUninformativeName 判断名称是否缺乏信息量（RomM 同款思路）
func isUninformativeName(s string) bool {
	t := strings.ToLower(strings.TrimSpace(s))
	if len([]rune(t)) < 2 {
		return true
	}
	switch t {
	case "dlc", "dlcs", "update", "updates", "patch", "patches",
		"switch", "ns", "nsp", "xci", "nsz", "games", "game", "rom", "roms",
		"disc", "disk", "cd", "dvd", "ps3", "ps4", "ps5":
		return true
	}
	return false
}

// CleanName 去掉括号标签与多余空白，得到干净的游戏名。
func CleanName(s string) string {
	s = genericTagRe.ReplaceAllString(s, " ")
	s = strings.Join(strings.Fields(s), " ")
	return strings.Trim(s, " -_·.")
}

// CleanFileName 先去掉扩展名再清洗，用于文件名（而非目录名）。
func CleanFileName(name string) string {
	return CleanName(strings.TrimSuffix(name, filepath.Ext(name)))
}

// DetectPlatform 把库内目录名映射为平台 slug；无法识别返回空串。
func DetectPlatform(dirName string) string {
	key := strings.ToLower(strings.TrimSpace(dirName))
	if slug, ok := platformAliases[key]; ok {
		return slug
	}
	compact := strings.NewReplacer(" ", "", "_", "", "-", "").Replace(key)
	return platformAliases[compact]
}

// CategoryFromDir 把子目录名映射为规范化分类；不是分类目录则返回空串。
func CategoryFromDir(dirName string) string {
	return CategoryDirs[strings.ToLower(strings.TrimSpace(dirName))]
}

// IsCategoryDir 判断目录名是否为 RomM 认可的分类子目录。
func IsCategoryDir(dirName string) bool {
	return CategoryFromDir(dirName) != ""
}

// Layout 库结构解析结果
type Layout struct {
	Platform    string   // ps3 / ps4 / ps5 / switch；无法判定为空串
	Category    string   // 规范化分类，默认 "game"
	GameName    string   // {game} 层名称
	IsMultiFile bool     // 是否位于游戏文件夹内（多文件游戏）
	RelParts    []string // 相对根目录的路径分段
}

// LayoutOptions 控制库结构解析
type LayoutOptions struct {
	// PlatformHint 该扫描根已知的平台 slug（可为空）
	PlatformHint string
	// CategoryLevel 该平台是否声明了 {category} 层级，
	// 例如 ps3: 'roms/{platform}/{category}/{game}'。
	// 开启后「平台目录下的第一层」视为组织用分类，游戏在其下一层。
	CategoryLevel bool
}

// DetectLayout 便捷版：不启用 {category} 层级。
func DetectLayout(root, filePath, platformHint string) Layout {
	return DetectLayoutWith(root, filePath, LayoutOptions{PlatformHint: platformHint})
}

// DetectLayoutWith 依据官方库结构模板 roms/{platform}/{game} 解析文件位置。
//
// 支持：
//   - roms/ps5/Game Name/update/patch.pkg    （含 roms 与平台层）
//   - ps5/Game Name/dlc/x.pkg                （以平台目录为根）
//   - /packages/ps5/Game.pkg                 （单文件游戏直接放在平台目录）
//   - roms/ps3/Redump/Game.iso               （CategoryLevel=true 时的 {category} 层）
func DetectLayoutWith(root, filePath string, opt LayoutOptions) Layout {
	l := Layout{Category: CategoryGame, Platform: opt.PlatformHint}

	rel, err := filepath.Rel(root, filePath)
	if err != nil || rel == "" || strings.HasPrefix(rel, "..") {
		rel = filepath.Base(filePath)
	}
	parts := make([]string, 0, 8)
	for _, s := range strings.Split(filepath.ToSlash(rel), "/") {
		if s != "" && s != "." {
			parts = append(parts, s)
		}
	}
	l.RelParts = parts

	i := 0
	// 可选的 roms 前缀
	if i < len(parts)-1 && strings.EqualFold(parts[i], "roms") {
		i++
	}
	// 可选的平台目录层
	if i < len(parts)-1 {
		if slug := DetectPlatform(parts[i]); slug != "" {
			l.Platform = slug
			i++
		}
	}
	// 根目录本身就是平台目录的情况（如扫描根配置为 /packages/ps5）
	if l.Platform == "" {
		l.Platform = DetectPlatform(filepath.Base(root))
	}

	rest := parts[i:]
	if len(rest) == 0 {
		return l
	}

	if opt.CategoryLevel && len(rest) >= 2 {
		// 第一层是组织用分类，游戏在其下一层
		rest = rest[1:]
	}

	if len(rest) == 1 {
		l.GameName = CleanFileName(rest[0])
		return l
	}

	l.IsMultiFile = true
	l.GameName = CleanName(rest[0])
	// 在游戏文件夹与文件之间寻找分类子目录
	for _, seg := range rest[1 : len(rest)-1] {
		if c := CategoryFromDir(seg); c != "" {
			l.Category = c
			break
		}
	}
	return l
}

func appendUnique(dst []string, vals ...string) []string {
	for _, v := range vals {
		if v == "" {
			continue
		}
		dup := false
		for _, e := range dst {
			if e == v {
				dup = true
				break
			}
		}
		if !dup {
			dst = append(dst, v)
		}
	}
	return dst
}

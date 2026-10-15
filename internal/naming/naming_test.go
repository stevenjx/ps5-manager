package naming

import (
	"reflect"
	"testing"
)

// ---------- 文件名标签 ----------

func TestParseFileName_PS3Redump(t *testing.T) {
	// Redump 风格：游戏名 + (地区) + (语言列表) + [TitleID]
	// 注意 (En,Fr,De,Es,It) 里 Fr/De/It 同时是语言码与地区码（法国/德国/意大利），
	// 按 RomM parse_tags 的裁定：首字母大写的两字母码走「精确语言码表」→ 判为语言。
	p := ParseFileName("Demon's Souls (Europe) (En,Fr,De,Es,It) [BLES00932].iso")

	if p.Name != "Demon's Souls" {
		t.Errorf("Name = %q, 期望 %q", p.Name, "Demon's Souls")
	}
	if !reflect.DeepEqual(p.Regions, []string{"eu"}) {
		t.Errorf("Regions = %v, 期望 [eu]", p.Regions)
	}
	if !reflect.DeepEqual(p.Languages, []string{"en", "fr", "de", "es", "it"}) {
		t.Errorf("Languages = %v, 期望 [en fr de es it]", p.Languages)
	}
}

func TestParseFileName_RegionVsLanguageCase(t *testing.T) {
	// RomM 用「大小写」区分同形码：全大写走地区码表，首字母大写走语言码表。
	// 参考 roms_handler.parse_tags 注释："some language shortcodes differ
	// from a region code only by case (Nl/NL, No/NO)"。
	cases := []struct {
		in      string
		wantReg string
		wantLan string
	}{
		{"Game (U).iso", "us", ""},  // 全大写 U = USA（地区）
		{"Game (J).iso", "jp", ""},  // 全大写 J = Japan（地区）
		{"Game (E).iso", "eu", ""},  // 全大写 E = Europe（地区）
		{"Game (NL).iso", "nl", ""}, // 全大写 NL = Netherlands（地区）
		{"Game (Nl).iso", "", "nl"}, // 首字母大写 Nl = Dutch（语言）
		{"Game (De).iso", "", "de"}, // De = German（语言，不是德国）
		{"Game (Fr).iso", "", "fr"}, // Fr = French
		{"Game (It).iso", "", "it"}, // It = Italian
		{"Game (En).iso", "", "en"}, // En = English
		{"Game (Ja).iso", "", "ja"}, // Ja = Japanese
		{"Game (Zh).iso", "", "zh"}, // Zh = Chinese
	}
	for _, c := range cases {
		p := ParseFileName(c.in)
		gotReg, gotLan := "", ""
		if len(p.Regions) > 0 {
			gotReg = p.Regions[0]
		}
		if len(p.Languages) > 0 {
			gotLan = p.Languages[0]
		}
		if gotReg != c.wantReg || gotLan != c.wantLan {
			t.Errorf("%q -> region=%q language=%q, 期望 region=%q language=%q",
				c.in, gotReg, gotLan, c.wantReg, c.wantLan)
		}
	}
}

func TestParseFileName_LowercaseNlNoStayRegion(t *testing.T) {
	// RomM 特例：nl / no 在 GoodTools 地区码表中本就存在，
	// 因此小写写法仍按地区处理（与 de/fr/it 不同）。
	p := ParseFileName("Game (nl).iso")
	if !reflect.DeepEqual(p.Regions, []string{"nl"}) {
		t.Errorf("(nl) 应为地区 Netherlands，得到 Regions=%v Languages=%v", p.Regions, p.Languages)
	}
	p = ParseFileName("Game (no).iso")
	if !reflect.DeepEqual(p.Regions, []string{"no"}) {
		t.Errorf("(no) 应为地区 Norway，得到 Regions=%v Languages=%v", p.Regions, p.Languages)
	}
}

func TestParseFileName_RegionFullNames(t *testing.T) {
	cases := map[string]string{
		"Game (USA).iso":           "us",
		"Game (Japan).iso":         "jp",
		"Game (World).iso":         "wor",
		"Game (Hong Kong).iso":     "hk",
		"Game (China).iso":         "cn",
		"Game (Korea).iso":         "kr",
		"Game (Taiwan).iso":        "tw",
		"Game (Europe).iso":        "eu",
		"Game (Germany).iso":       "de",
		"Game (United States).iso": "", // 非 RomM 枚举值，不应被识别为地区
	}
	for in, want := range cases {
		p := ParseFileName(in)
		got := ""
		if len(p.Regions) > 0 {
			got = p.Regions[0]
		}
		if got != want {
			t.Errorf("ParseFileName(%q).Regions[0] = %q, 期望 %q", in, got, want)
		}
	}
}

func TestParseFileName_LanguageFullNames(t *testing.T) {
	p := ParseFileName("Game (Japanese).iso")
	if !reflect.DeepEqual(p.Languages, []string{"ja"}) {
		t.Errorf("(Japanese) 应为语言 ja，得到 %v", p.Languages)
	}
}

func TestParseFileName_InlineProviderTag(t *testing.T) {
	p := ParseFileName("Metal Gear Solid 4 (USA) (igdb-1064).iso")
	if p.ProviderTag != "igdb" || p.ProviderID != "1064" {
		t.Errorf("ProviderTag/ID = %q/%q, 期望 igdb/1064", p.ProviderTag, p.ProviderID)
	}
	if p.ProviderRef() != "igdb:1064" {
		t.Errorf("ProviderRef = %q", p.ProviderRef())
	}
	if p.Name != "Metal Gear Solid 4" {
		t.Errorf("Name = %q", p.Name)
	}
	if !reflect.DeepEqual(p.Regions, []string{"us"}) {
		t.Errorf("Regions = %v, 期望 [us]", p.Regions)
	}
}

func TestParseFileName_SwitchTitleIDAndVersion(t *testing.T) {
	p := ParseFileName("The Legend of Zelda - Tears of the Kingdom [0100F2C0115B6000][v0].nsp")
	if p.SwitchTitleID != "0100F2C0115B6000" {
		t.Errorf("SwitchTitleID = %q", p.SwitchTitleID)
	}
	if p.Version != "0" {
		t.Errorf("Version = %q, 期望 0", p.Version)
	}
	if p.Name != "The Legend of Zelda - Tears of the Kingdom" {
		t.Errorf("Name = %q", p.Name)
	}
}

func TestParseFileName_RevisionAndVersion(t *testing.T) {
	p := ParseFileName("Game (USA) (rev v1).iso")
	if p.Revision != "v1" {
		t.Errorf("Revision = %q, 期望 v1", p.Revision)
	}
	p = ParseFileName("Game (USA) (v1.2).iso")
	if p.Version != "1.2" {
		t.Errorf("Version = %q, 期望 1.2", p.Version)
	}
}

func TestParseFileName_TranslationTag(t *testing.T) {
	// GoodTools 汉化标签：[T+Chi]
	p := ParseFileName("Game (Japan) [T+Chi].iso")
	if !reflect.DeepEqual(p.Languages, []string{"zh"}) {
		t.Errorf("[T+Chi] 应识别出语言 zh，得到 %v", p.Languages)
	}
	// TOSEC 风格：[tr fr]
	p = ParseFileName("Game (Europe) [tr fr].iso")
	if !reflect.DeepEqual(p.Languages, []string{"fr"}) {
		t.Errorf("[tr fr] 应识别出语言 fr，得到 %v", p.Languages)
	}
}

func TestParseFileName_NonProviderTagIgnored(t *testing.T) {
	// launchbox 是合法 provider 标签；mobygames 不是（RomM 用 moby）
	p := ParseFileName("Game (mobygames-42).iso")
	if p.ProviderRef() != "" {
		t.Errorf("非枚举 provider 标签不应被识别，得到 %q", p.ProviderRef())
	}
	p = ParseFileName("Game (launchbox-42).iso")
	if p.ProviderRef() != "launchbox:42" {
		t.Errorf("launchbox 标签应被识别，得到 %q", p.ProviderRef())
	}
}

func TestParseFileName_CustomRegion(t *testing.T) {
	p := ParseFileName("Game (reg MyOwnLang).iso")
	if !reflect.DeepEqual(p.Regions, []string{"myownlang"}) {
		t.Errorf("(reg X) 应保留为地区，得到 %v", p.Regions)
	}
}

// ---------- 路径解析 ----------

func TestParsePath_UpdateFileUsesGameFolderName(t *testing.T) {
	// 更新补丁文件的游戏名应取自游戏文件夹，而不是 "patch"
	p := ParsePath("/library/ps5/God of War (2018) (USA) [CUSA07408]/update/patch.pkg")
	if p.Name != "God of War" {
		t.Errorf("Name = %q, 期望 %q（应取游戏文件夹名）", p.Name, "God of War")
	}
	if !reflect.DeepEqual(p.Regions, []string{"us"}) {
		t.Errorf("Regions = %v, 期望 [us]", p.Regions)
	}
}

func TestParsePath_SwitchDLCFile(t *testing.T) {
	p := ParsePath("/library/roms/switch/Zelda [0100F2C0115B6000]/dlc/zelda_dlc_1.nsp")
	if p.Name != "Zelda" {
		t.Errorf("Name = %q, 期望 Zelda", p.Name)
	}
	if p.SwitchTitleID != "0100F2C0115B6000" {
		t.Errorf("SwitchTitleID = %q", p.SwitchTitleID)
	}
}

// ---------- 平台 / 分类 ----------

func TestDetectPlatform(t *testing.T) {
	cases := map[string]string{
		"ps3": "ps3", "PS3": "ps3", "PlayStation 3": "ps3",
		"ps4": "ps4", "PS4": "ps4", "PlayStation 4": "ps4",
		"ps5": "ps5", "PS5": "ps5", "PlayStation 5": "ps5",
		"switch": "switch", "Switch": "switch", "Nintendo Switch": "switch",
		"xbox": "", "unknown": "",
	}
	for in, want := range cases {
		if got := DetectPlatform(in); got != want {
			t.Errorf("DetectPlatform(%q) = %q, 期望 %q", in, got, want)
		}
	}
}

func TestCategoryFromDir(t *testing.T) {
	cases := map[string]string{
		"update": CategoryUpdate, "Updates": CategoryUpdate,
		"dlc": CategoryDLC, "DLCs": CategoryDLC,
		"patch": CategoryPatch, "hack": CategoryHack, "mod": CategoryMod,
		"translation": CategoryTranslation, "manual": CategoryManual,
		"Game Name": "", "roms": "",
	}
	for in, want := range cases {
		if got := CategoryFromDir(in); got != want {
			t.Errorf("CategoryFromDir(%q) = %q, 期望 %q", in, got, want)
		}
	}
}

// ---------- 库结构 ----------

func TestDetectLayout_SwitchMultiFile(t *testing.T) {
	l := DetectLayout(
		"/library",
		"/library/roms/switch/The Legend of Zelda [0100F2C0115B6000]/dlc/zelda_dlc.nsp",
		"",
	)
	if l.Platform != PlatformSwitch {
		t.Errorf("Platform = %q, 期望 switch", l.Platform)
	}
	if l.Category != CategoryDLC {
		t.Errorf("Category = %q, 期望 dlc", l.Category)
	}
	if !l.IsMultiFile {
		t.Error("IsMultiFile 应为 true")
	}
	if l.GameName == "" {
		t.Error("GameName 不应为空")
	}
}

func TestDetectLayout_PS5Update(t *testing.T) {
	l := DetectLayout(
		"/packages/ps5",
		"/packages/ps5/God of War (2018) (USA)/update/CUSA07408 - God of War [v01.33].pkg",
		PlatformPS5,
	)
	if l.Platform != PlatformPS5 {
		t.Errorf("Platform = %q, 期望 ps5", l.Platform)
	}
	if l.Category != CategoryUpdate {
		t.Errorf("Category = %q, 期望 update", l.Category)
	}
}

func TestDetectLayout_SingleFileGame(t *testing.T) {
	// 扫描根本身就是平台目录
	l := DetectLayout(
		"/library/roms/ps3",
		"/library/roms/ps3/Demon's Souls (Europe) [BLES00932].iso",
		"",
	)
	if l.Platform != PlatformPS3 {
		t.Errorf("Platform = %q, 期望 ps3（应能从扫描根目录名推断）", l.Platform)
	}
	if l.IsMultiFile {
		t.Error("单文件游戏 IsMultiFile 应为 false")
	}
	if l.Category != CategoryGame {
		t.Errorf("Category = %q, 期望 game", l.Category)
	}
	if l.GameName != "Demon's Souls" {
		t.Errorf("GameName = %q, 期望 %q", l.GameName, "Demon's Souls")
	}
}

func TestDetectLayout_PS3CategoryLevelOption(t *testing.T) {
	// 默认模板 roms/{platform}/{game}：Redump 是游戏文件夹
	l := DetectLayout("/library", "/library/roms/ps3/Redump/Game (USA).iso", "")
	if l.GameName != "Redump" {
		t.Errorf("默认模板下 GameName = %q, 期望 Redump", l.GameName)
	}

	// 声明了 ps3: roms/{platform}/{category}/{game} 时，Redump 是分类层
	l = DetectLayoutWith("/library", "/library/roms/ps3/Redump/Game (USA).iso",
		LayoutOptions{CategoryLevel: true})
	if l.GameName != "Game" {
		t.Errorf("{category} 层级下 GameName = %q, 期望 Game", l.GameName)
	}
	if l.Platform != PlatformPS3 {
		t.Errorf("Platform = %q, 期望 ps3", l.Platform)
	}
}

func TestCleanName(t *testing.T) {
	cases := map[string]string{
		"Game Name (USA) (En)": "Game Name",
		"Game  [v1.0]  Name":   "Game Name",
		"  spaced   out  ":     "spaced out",
	}
	for in, want := range cases {
		if got := CleanName(in); got != want {
			t.Errorf("CleanName(%q) = %q, 期望 %q", in, got, want)
		}
	}
}

func TestParseFileName_PS3Serial(t *testing.T) {
	p := ParseFileName("Demon's Souls (Europe) (En,Fr,De) [BLES00932].iso")
	if p.PSSerial != "BLES00932" {
		t.Errorf("PSSerial = %q, 期望 BLES00932", p.PSSerial)
	}
	if p.Name != "Demon's Souls" {
		t.Errorf("Name = %q", p.Name)
	}
	// PSN 风格序列号
	p = ParseFileName("The Last of Us (USA) [BCUS98174].iso")
	if p.PSSerial != "BCUS98174" {
		t.Errorf("PSSerial = %q", p.PSSerial)
	}
	// 目录名里的序列号也应被继承
	p = ParsePath("/library/roms/ps3/Game (Japan) [NPUB30123]/disc1.iso")
	if p.PSSerial != "NPUB30123" {
		t.Errorf("ParsePath PSSerial = %q", p.PSSerial)
	}
	// 不应误判 Switch 的 16 位十六进制 TitleID
	p = ParseFileName("Game [0100F2C0115B6000].nsp")
	if p.PSSerial != "" {
		t.Errorf("Switch TitleID 不应被当作 PS3 序列号，得到 %q", p.PSSerial)
	}
}

func TestParsePath_SingleFileDirectlyInPlatformDir(t *testing.T) {
	// 单文件游戏直接放在平台目录：游戏名必须来自文件名，
	// 不能被库根目录名（如 "gmtest"）污染。
	p := ParsePath("/tmp/gmtest/games/ps3/Demon's Souls (Europe) (En,Fr,De,Es,It) [BLES00932].iso")
	if p.Name != "Demon's Souls" {
		t.Errorf("Name = %q, 期望 Demon's Souls", p.Name)
	}
	if p.PSSerial != "BLES00932" {
		t.Errorf("PSSerial = %q", p.PSSerial)
	}
	if !reflect.DeepEqual(p.Regions, []string{"eu"}) {
		t.Errorf("Regions = %v, 期望 [eu]", p.Regions)
	}
	if len(p.Languages) != 5 {
		t.Errorf("Languages = %v, 期望 5 项", p.Languages)
	}
}

func TestParsePath_LibraryRootNamesNotTreatedAsGame(t *testing.T) {
	// roms/{platform}/{game} 结构：平台目录之上的一切都不应成为游戏名
	for _, in := range []string{
		"/library/roms/ps3/Game (USA).iso",
		"/volume1/roms/ps3/Game (USA).iso",
		"/data/games/ps3/Game (USA).iso",
	} {
		if got := ParsePath(in).Name; got != "Game" {
			t.Errorf("ParsePath(%q).Name = %q, 期望 Game", in, got)
		}
	}
}

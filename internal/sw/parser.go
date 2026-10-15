// Package sw 提供 Nintendo Switch 游戏文件（XCI / NSP / NSZ）的解析能力。
//
// 设计原则：
//   - 扫描阶段只做「快速元数据」解析（容器头 + 文件名），保证大库扫描速度；
//   - 图标提取较慢（需在 NCA 内定位 control 数据），改为按需调用（前端请求图标时）；
//   - 官方 NSP/XCI 的 NCA 通常加密，无法读取 NACP 元数据，此时以文件名/目录名
//     作为名称来源，并给出占位图标，保证「能列出、有名称、有版本、有 DLC 统计」。
package sw

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Info 一个 Switch 游戏文件的解析结果
type Info struct {
	TitleID    string // 基础 TitleID（DLC 会归一到本体，便于聚合）
	RawTitleID string // 文件原始 TitleID
	ContentID  string // 原始 TitleID（兼容 store.ContentID 字段）
	Title      string
	Version    string
	Category   string // "game" | "dlc" | "patch"
	Platform   string // "SWITCH"
	Region     string
	FileSize   int64
	Container  string // "NSP" | "XCI" | "NSZ" | ""
	FileCount  int    // 容器内文件数（NCA 等）
}

var (
	titleIDRe = regexp.MustCompile(`\[([0-9A-Fa-f]{16})\]`)
	// 形如 [v65536]、[v0]
	versionRe = regexp.MustCompile(`\[v(\d{1,10})\]`)
	// 无括号的裸 TitleID（部分命名直接写 0100xxxxxxxxxxxx）
	bareTitleIDRe = regexp.MustCompile(`\b(01[0-9A-Fa-f]{14})\b`)
)

// Parse 快速解析 Switch 游戏文件（只读少量头部字节）
func Parse(filePath string) (*Info, error) {
	fi, err := os.Stat(filePath)
	if err != nil {
		return nil, err
	}

	info := &Info{
		Platform: "SWITCH",
		Region:   "全区",
		FileSize: fi.Size(),
		Version:  "v1.0.0",
		Category: "game",
	}

	// 1) 文件名 / 目录名元数据（最可靠的一手信息）
	parseFromName(filePath, info)

	// 2) 容器头解析（补充容器类型、文件数、必要时补 TitleID）
	f, err := os.Open(filePath)
	if err == nil {
		defer f.Close()
		parseContainer(f, info)
	}

	// 3) 归一化：把 DLC 的 TitleID 归到本体，便于与本体聚合
	info.RawTitleID = info.TitleID
	if info.TitleID != "" {
		info.TitleID = BaseTitleID(info.TitleID)
	}
	info.ContentID = info.RawTitleID

	if info.Title == "" {
		if info.RawTitleID != "" {
			info.Title = info.RawTitleID
		} else {
			info.Title = strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
		}
	}
	return info, nil
}

// parseFromName 从文件名与父目录名推断 TitleID / 版本 / 名称 / 分类
func parseFromName(filePath string, info *Info) {
	base := filepath.Base(filePath)
	name := strings.TrimSuffix(base, filepath.Ext(base))

	// TitleID
	if m := titleIDRe.FindStringSubmatch(name); len(m) == 2 {
		info.TitleID = strings.ToUpper(m[1])
	} else if m := bareTitleIDRe.FindStringSubmatch(name); len(m) == 2 {
		info.TitleID = strings.ToUpper(m[1])
	}

	// 版本
	if m := versionRe.FindStringSubmatch(name); len(m) == 2 {
		if v, err := strconv.Atoi(m[1]); err == nil {
			info.Version = FormatVersion(v)
		}
	}

	// 名称：去掉 [..] 与常见后缀后的主体；为空则用父目录名
	clean := cleanGameName(name)
	if clean == "" {
		parent := filepath.Base(filepath.Dir(filePath))
		clean = cleanGameName(parent)
	}
	// 名称仍无信息量时（例如文件与目录都叫 DLC），按序号生成可读名
	if isUninformativeName(clean) && info.TitleID != "" {
		switch ClassifyTitleID(info.TitleID) {
		case "dlc":
			clean = fmt.Sprintf("追加内容 #%d", DLCIndex(info.TitleID))
		case "patch":
			clean = "更新补丁"
		}
	}
	info.Title = clean

	// 分类（若已拿到 TitleID）
	if info.TitleID != "" {
		switch ClassifyTitleID(info.TitleID) {
		case "dlc":
			info.Category = "dlc"
		case "patch":
			info.Category = "patch"
		default:
			info.Category = "game"
		}
	}
}

// isUninformativeName 判断名称是否缺乏信息量（空 / 过于笼统 / 纯目录语义）
func isUninformativeName(s string) bool {
	t := strings.ToLower(strings.TrimSpace(s))
	if len([]rune(t)) < 2 {
		return true
	}
	switch t {
	case "dlc", "switch", "ns", "nsp", "xci", "nsz", "games", "game", "rom", "roms",
		"游戏", "追加内容", "update", "updates", "patch", "patches":
		return true
	}
	return false
}

// DLCIndex 从 TitleID 低 12 位提取 DLC 序号（本体 0x000 / 更新 0x800 之外的递增序号）
func DLCIndex(id string) int {
	v, err := strconv.ParseUint(strings.TrimSpace(id), 16, 64)
	if err != nil {
		return 0
	}
	return int(v & 0xFFF)
}

// cleanGameName 去掉方括号内容与来源站后缀，得到较干净的游戏名
func cleanGameName(s string) string {
	// 去掉所有 [..] / （..） 内容
	s = regexp.MustCompile(`\[[^\]]*\]`).ReplaceAllString(s, " ")
	s = regexp.MustCompile(`\([^)]*\)`).ReplaceAllString(s, " ")
	// 常见来源站后缀
	for _, tag := range []string{"NSP", "XCI", "NSZ", "nsp", "xci", "nsz"} {
		s = strings.ReplaceAll(s, tag, " ")
	}
	for _, suffix := range []string{"-DODI", "FitGirl", "[Switch]", "SWITCH"} {
		s = strings.ReplaceAll(s, suffix, " ")
	}
	s = strings.Join(strings.Fields(s), " ")
	s = strings.Trim(s, " -_·.")
	return s
}

// ---------- 容器解析 ----------

type fsEntry struct {
	name   string
	offset uint64
	size   uint64
}

// parseContainer 识别 NSP(PFS0) / XCI / NSZ 并填充容器信息
func parseContainer(f *os.File, info *Info) {
	hdr := make([]byte, 0x200)
	n, err := f.ReadAt(hdr, 0)
	if n <= 0 {
		return
	}
	// 注意：小文件会返回 io.EOF，但仍读到了有效数据，需以 n 为准裁剪
	_ = err
	hdr = hdr[:n]

	if len(hdr) >= 4 && string(hdr[0:4]) == "PFS0" {
		info.Container = "NSP"
		if entries, err := parsePFS0(f, 0); err == nil {
			info.FileCount = len(entries)
		}
		return
	}
	if len(hdr) >= 0x104 && string(hdr[0x100:0x104]) == "HEAD" {
		// XCI 游戏卡镜像
		info.Container = "XCI"
		info.FileCount = countXCIContents(f)
		return
	}
	// 其余情况（含 .nsz 压缩容器）：仅凭文件名推断
	info.Container = "NSZ"
}

// parsePFS0 解析 PFS0 文件系统（NSP 主容器），base 为容器起始偏移
func parsePFS0(f io.ReaderAt, base int64) ([]fsEntry, error) {
	hdr := make([]byte, 0x10)
	if _, err := f.ReadAt(hdr, base); err != nil {
		return nil, err
	}
	if string(hdr[0:4]) != "PFS0" {
		return nil, fmt.Errorf("not PFS0")
	}
	count := binary.LittleEndian.Uint32(hdr[4:8])
	strSize := binary.LittleEndian.Uint32(hdr[8:12])
	if count == 0 || count > 8192 || strSize > 1<<20 {
		return nil, fmt.Errorf("bad PFS0 header")
	}

	entriesRaw := make([]byte, int(count)*0x18)
	if _, err := f.ReadAt(entriesRaw, base+0x10); err != nil {
		return nil, err
	}
	strTable := make([]byte, strSize)
	if _, err := f.ReadAt(strTable, base+0x10+int64(count)*0x18); err != nil {
		return nil, err
	}
	dataStart := base + 0x10 + int64(count)*0x18 + int64(strSize)

	entries := make([]fsEntry, 0, count)
	for i := 0; i < int(count); i++ {
		e := entriesRaw[i*0x18 : (i+1)*0x18]
		off := binary.LittleEndian.Uint64(e[0:8])
		size := binary.LittleEndian.Uint64(e[8:16])
		nameOff := binary.LittleEndian.Uint32(e[16:20])
		entries = append(entries, fsEntry{
			name:   cstr(strTable, nameOff),
			offset: uint64(dataStart) + off,
			size:   size,
		})
	}
	return entries, nil
}

// parseHFS0 解析 HFS0 文件系统（XCI 分区），返回条目及其数据区绝对偏移
func parseHFS0(f io.ReaderAt, base int64) ([]fsEntry, int64, error) {
	hdr := make([]byte, 0x10)
	if _, err := f.ReadAt(hdr, base); err != nil {
		return nil, 0, err
	}
	if string(hdr[0:4]) != "HFS0" {
		return nil, 0, fmt.Errorf("not HFS0")
	}
	count := binary.LittleEndian.Uint32(hdr[4:8])
	strSize := binary.LittleEndian.Uint32(hdr[8:12])
	if count == 0 || count > 256 || strSize > 1<<20 {
		return nil, 0, fmt.Errorf("bad HFS0 header")
	}
	entriesRaw := make([]byte, int(count)*0x40)
	if _, err := f.ReadAt(entriesRaw, base+0x10); err != nil {
		return nil, 0, err
	}
	strTable := make([]byte, strSize)
	if _, err := f.ReadAt(strTable, base+0x10+int64(count)*0x40); err != nil {
		return nil, 0, err
	}
	dataStart := base + 0x10 + int64(count)*0x40 + int64(strSize)

	entries := make([]fsEntry, 0, count)
	for i := 0; i < int(count); i++ {
		e := entriesRaw[i*0x40 : (i+1)*0x40]
		off := binary.LittleEndian.Uint64(e[0:8])
		size := binary.LittleEndian.Uint64(e[8:16])
		nameOff := binary.LittleEndian.Uint32(e[16:20])
		entries = append(entries, fsEntry{
			name:   cstr(strTable, nameOff),
			offset: uint64(dataStart) + off,
			size:   size,
		})
	}
	return entries, dataStart, nil
}

// countXCIContents 统计 XCI「secure」分区内的 NCA 数量
func countXCIContents(f io.ReaderAt) int {
	hdr := make([]byte, 0x200)
	if _, err := f.ReadAt(hdr, 0); err != nil {
		return 0
	}
	if string(hdr[0x100:0x104]) != "HEAD" {
		return 0
	}
	// 0x130 处为根 HFS0 偏移
	rootOff := int64(binary.LittleEndian.Uint64(hdr[0x130:0x138]))
	if rootOff <= 0 {
		return 0
	}
	root, _, err := parseHFS0(f, rootOff)
	if err != nil {
		return 0
	}
	for _, p := range root {
		// 游戏内容位于 secure 分区
		if strings.EqualFold(p.name, "secure") {
			if sec, _, err := parseHFS0(f, int64(p.offset)); err == nil {
				return len(sec)
			}
		}
	}
	// 若找不到 secure（某些镜像直接放 NCA），退回根条目数
	return len(root)
}

func cstr(b []byte, off uint32) string {
	if int(off) >= len(b) {
		return ""
	}
	end := bytes.IndexByte(b[off:], 0)
	if end < 0 {
		return string(b[off:])
	}
	return string(b[off : off+uint32(end)])
}

// ---------- TitleID 语义 ----------

// ClassifyTitleID 依据 TitleID 低 12 位判定内容类型：
//
//	低 12 位 == 0x000 → 游戏本体
//	低 12 位 == 0x800 → 更新补丁
//	其余（0x1000 递增）→ 追加内容 DLC
//
// 例如《塞尔达传说：王国之泪》本体 0100F2C0115B6000，
// 其 DLC 为 0100F2C0115B7001 / ...7002（本体 + 0x1000 + 序号）。
func ClassifyTitleID(id string) string {
	v, err := strconv.ParseUint(strings.TrimSpace(id), 16, 64)
	if err != nil {
		return "game"
	}
	low := v & 0xFFF
	switch {
	case low == 0x000:
		return "game"
	case low == 0x800:
		return "patch"
	default:
		return "dlc"
	}
}

// BaseTitleID 返回归一化到「本体」的 TitleID（DLC 会减去 0x1000），
// 使同一游戏的多个 DLC 与本体共享同一聚合键。
func BaseTitleID(id string) string {
	v, err := strconv.ParseUint(strings.TrimSpace(id), 16, 64)
	if err != nil {
		return strings.ToUpper(id)
	}
	low := v & 0xFFF
	base := v &^ 0xFFF
	if low != 0x000 && low != 0x800 {
		base -= 0x1000
	}
	return fmt.Sprintf("%016X", base)
}

// FormatVersion 把 Switch 版本整数格式化为 vX.Y.Z。
// 编码为大版本<<16 | 次版本<<8 | 修订号；0 视作 1.0.0（未打补丁的初始版本）。
func FormatVersion(v int) string {
	if v <= 0 {
		return "v1.0.0"
	}
	major := (v >> 16) & 0xFFFF
	minor := (v >> 8) & 0xFF
	micro := v & 0xFF
	return fmt.Sprintf("v%d.%d.%d", major, minor, micro)
}

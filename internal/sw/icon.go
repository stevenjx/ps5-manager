package sw

import (
	"bytes"
	"io"
	"os"
)

const (
	// nacpMagic NACP（Nintendo Application Control Property）魔数
	nacpMagic = "NACP"
	// 搜索上限：避免在超大文件上全盘扫描
	maxNacpSearch = 128 << 20
	// 分块大小
	searchChunk = 4 << 20
	// 找到 NACP 后，在其之后多大范围内寻找图标 JPEG
	iconSearchWindow = 16 << 20
)

var (
	jpegSOI = []byte{0xFF, 0xD8, 0xFF}
	jpegEOI = []byte{0xFF, 0xD9}
)

// ExtractIcon 尽力从 Switch 游戏文件中提取图标 JPEG。
//
// 说明：官方 NSP/XCI 的 NCA 通常加密，control 数据不可读，此时返回 nil，
// 由调用方回退到占位图。对「已解密」的 dump，control NACP 及其附属
// icon_*.dat(JPEG) 为明文，这里通过定位 "NACP" 魔数并在其后搜索 JPEG 提取。
func ExtractIcon(filePath string) []byte {
	f, err := os.Open(filePath)
	if err != nil {
		return nil
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil
	}
	limit := fi.Size()
	if limit > maxNacpSearch {
		limit = maxNacpSearch
	}

	pos := findMagic(f, []byte(nacpMagic), limit)
	if pos < 0 {
		return nil
	}

	// 在 NACP 之后搜索 JPEG 图标
	winStart := pos
	winEnd := pos + int64(iconSearchWindow)
	if winEnd > fi.Size() {
		winEnd = fi.Size()
	}
	if winEnd <= winStart {
		return nil
	}
	return findJPEG(f, winStart, winEnd)
}

// findMagic 在前 limit 字节内分块查找 magic，返回绝对偏移；未找到返回 -1
func findMagic(f *os.File, magic []byte, limit int64) int64 {
	buf := make([]byte, searchChunk)
	var offset int64
	overlap := len(magic) - 1

	for offset < limit {
		n, err := f.ReadAt(buf, offset)
		if n <= 0 {
			break
		}
		if idx := bytes.Index(buf[:n], magic); idx >= 0 {
			return offset + int64(idx)
		}
		if err != nil {
			break
		}
		step := int64(n) - int64(overlap)
		if step <= 0 {
			break
		}
		offset += step
	}
	return -1
}

// findJPEG 在 [start,end) 区间内寻找一段合法的 JPEG，返回其字节内容
func findJPEG(f *os.File, start, end int64) []byte {
	const readSpan = 8 << 20 // 每次读取窗口
	const maxIconSize = 4 << 20

	cursor := start
	for cursor < end {
		span := int64(readSpan)
		if cursor+span > end {
			span = end - cursor
		}
		buf := make([]byte, span)
		n, _ := f.ReadAt(buf, cursor)
		if n <= 0 {
			break
		}
		buf = buf[:n]

		idx := bytes.Index(buf, jpegSOI)
		if idx < 0 {
			cursor += int64(n) - 2
			if cursor <= start {
				cursor = start + 1
			}
			continue
		}
		data := buf[idx:]
		// 找到结尾 EOI
		if endIdx := bytes.Index(data[3:], jpegEOI); endIdx >= 0 {
			jpg := data[:3+endIdx+2]
			if len(jpg) > 2048 && len(jpg) <= maxIconSize {
				return jpg
			}
		}
		// 本窗口内未闭合，尝试从该位置跨窗口读取一段
		jpg := readCandidate(f, cursor+int64(idx), maxIconSize)
		if len(jpg) > 2048 {
			return jpg
		}
		cursor += int64(idx) + 3
	}
	return nil
}

// readCandidate 从 pos 起读取最多 max 字节，并裁剪到第一个 EOI
func readCandidate(f io.ReaderAt, pos int64, max int) []byte {
	buf := make([]byte, max)
	n, _ := f.ReadAt(buf, pos)
	if n <= 0 {
		return nil
	}
	buf = buf[:n]
	if endIdx := bytes.Index(buf[3:], jpegEOI); endIdx >= 0 {
		return buf[:3+endIdx+2]
	}
	return nil
}

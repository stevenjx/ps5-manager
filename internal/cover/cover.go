// Package cover 统一管理「竖版封面」在磁盘上的落地文件，
// 并提供图片格式 / 尺寸探测工具，供 pkg、scanner、server 共用。
package cover

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// exts 覆盖大小写变体，避免因扩展名不一致而找不到已存在的封面
var exts = []string{".jpg", ".jpeg", ".png", ".webp", ".JPG", ".JPEG", ".PNG", ".WEBP"}

// Store 管理封面目录的读写
type Store struct {
	dir string
}

func NewStore(dir string) *Store { return &Store{dir: dir} }

// Dir 返回封面存放目录
func (s *Store) Dir() string { return s.dir }

// Sub 返回同一封面根目录下的子目录 store。
//
// 用于把「不同用途」的图片分开存放，避免互相覆盖：
// Switch 的方形图标、16:9 横幅和 2:3 封面在过去共用同一个文件名，
// 导致 mode=cover / mode=icon 无法返回不同内容。
func (s *Store) Sub(kind string) *Store {
	if s == nil || s.dir == "" || kind == "" {
		return s
	}
	return &Store{dir: filepath.Join(s.dir, kind)}
}

func candidateNames(titleID string, id int64) []string {
	var names []string
	if titleID != "" {
		names = append(names, titleID, strings.ToUpper(titleID), strings.ToLower(titleID))
	}
	if id > 0 {
		names = append(names, strconv.FormatInt(id, 10))
	}
	return names
}

// Find 返回已存在的封面文件路径；未找到返回空串
func (s *Store) Find(titleID string, id int64) string {
	if s.dir == "" {
		return ""
	}
	for _, name := range candidateNames(titleID, id) {
		if name == "" {
			continue
		}
		for _, ext := range exts {
			target := filepath.Join(s.dir, name+ext)
			if fi, err := os.Stat(target); err == nil && !fi.IsDir() && fi.Size() > 0 {
				return target
			}
		}
	}
	return ""
}

// Exists 是否已有封面文件
func (s *Store) Exists(titleID string, id int64) bool {
	return s.Find(titleID, id) != ""
}

// Remove 删除所有可能的封面副本（不同扩展名 / 大小写）
func (s *Store) Remove(titleID string, id int64) {
	if s.dir == "" {
		return
	}
	for _, name := range candidateNames(titleID, id) {
		if name == "" {
			continue
		}
		for _, ext := range exts {
			_ = os.Remove(filepath.Join(s.dir, name+ext))
		}
	}
}

// Save 保存封面二进制，按 magic byte 自动选择扩展名
func (s *Store) Save(titleID string, id int64, data []byte) error {
	if s.dir == "" {
		return fmt.Errorf("empty cover dir")
	}
	if len(data) == 0 {
		return fmt.Errorf("empty cover data")
	}
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return err
	}
	s.Remove(titleID, id)
	target := titleID
	if target == "" {
		target = strconv.FormatInt(id, 10)
	}
	return os.WriteFile(filepath.Join(s.dir, target+DetectExt(data)), data, 0644)
}

// DetectExt 根据 magic byte 判断图片扩展名
func DetectExt(data []byte) string {
	switch {
	case len(data) >= 8 && data[0] == 0x89 && data[1] == 'P' && data[2] == 'N' && data[3] == 'G':
		return ".png"
	case len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return ".webp"
	case len(data) >= 2 && data[0] == 0xFF && data[1] == 0xD8:
		return ".jpg"
	}
	return ".jpg"
}

// Dimensions 读取图片的像素宽高（支持 JPEG / PNG）。
//
// 仅用于判断一张图是方形图标还是竖版封面，解析失败时返回 (0, 0)。
func Dimensions(data []byte) (int, int) {
	if w, h := pngSize(data); w > 0 {
		return w, h
	}
	return jpegSize(data)
}

// IsSquareish 判断图片是否接近 1:1（容差 15%）。
// Switch 的官方图标是正方形，竖版封面是 2:3，用这个基准可以把两者区分开。
func IsSquareish(data []byte) bool {
	w, h := Dimensions(data)
	if w <= 0 || h <= 0 {
		return false
	}
	ratio := float64(w) / float64(h)
	return ratio > 0.87 && ratio < 1.15
}

func pngSize(data []byte) (int, int) {
	// PNG: 8 字节签名 + IHDR（长度 4 + 类型 4 + 宽 4 + 高 4）
	if len(data) < 24 || data[0] != 0x89 || data[1] != 'P' || data[2] != 'N' || data[3] != 'G' {
		return 0, 0
	}
	w := int(data[16])<<24 | int(data[17])<<16 | int(data[18])<<8 | int(data[19])
	h := int(data[20])<<24 | int(data[21])<<16 | int(data[22])<<8 | int(data[23])
	return w, h
}

func jpegSize(data []byte) (int, int) {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 0, 0
	}
	i := 2
	for i+9 < len(data) {
		if data[i] != 0xFF {
			i++
			continue
		}
		marker := data[i+1]
		// 无长度字段的独立标记
		if marker == 0xD8 || marker == 0xD9 || (marker >= 0xD0 && marker <= 0xD7) || marker == 0x01 {
			i += 2
			continue
		}
		segLen := int(data[i+2])<<8 | int(data[i+3])
		// SOF0..SOF15（排除 DHT 0xC4 / JPG 0xC8 / DAC 0xCC）
		if marker >= 0xC0 && marker <= 0xCF && marker != 0xC4 && marker != 0xC8 && marker != 0xCC {
			if i+9 < len(data) {
				h := int(data[i+5])<<8 | int(data[i+6])
				w := int(data[i+7])<<8 | int(data[i+8])
				return w, h
			}
			return 0, 0
		}
		if segLen <= 0 {
			return 0, 0
		}
		i += 2 + segLen
	}
	return 0, 0
}

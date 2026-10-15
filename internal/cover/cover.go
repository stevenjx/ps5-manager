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

func (s *Store) candidateNames(titleID string, id int64) []string {
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
	for _, name := range s.candidateNames(titleID, id) {
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
	for _, name := range s.candidateNames(titleID, id) {
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

package cover

import "testing"

// pngHeader 构造一个只含签名 + IHDR 的最小 PNG 头，用于尺寸解析测试。
func pngHeader(w, h uint32) []byte {
	b := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	b = append(b, 0, 0, 0, 13, 'I', 'H', 'D', 'R')
	b = append(b,
		byte(w>>24), byte(w>>16), byte(w>>8), byte(w),
		byte(h>>24), byte(h>>16), byte(h>>8), byte(h),
	)
	b = append(b, 8, 6, 0, 0, 0) // bit depth / color type / 其余占位
	return b
}

// jpegHeader 构造一个只含 SOI + APP0 + SOF0 的最小 JPEG 头。
func jpegHeader(w, h int) []byte {
	b := []byte{0xFF, 0xD8}
	// APP0 段（长度 16，内容随意）
	b = append(b, 0xFF, 0xE0, 0x00, 0x10)
	b = append(b, []byte("JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00")...)
	// SOF0：长度 17，精度 8，高 2 字节，宽 2 字节，3 个分量
	b = append(b, 0xFF, 0xC0, 0x00, 0x11, 0x08,
		byte(h>>8), byte(h), byte(w>>8), byte(w), 0x03)
	b = append(b, 0x01, 0x22, 0x00, 0x02, 0x11, 0x01, 0x03, 0x11, 0x01)
	return b
}

func TestDimensionsPNG(t *testing.T) {
	w, h := Dimensions(pngHeader(600, 900))
	if w != 600 || h != 900 {
		t.Fatalf("PNG 尺寸 = %dx%d, 期望 600x900", w, h)
	}
}

func TestDimensionsJPEG(t *testing.T) {
	w, h := Dimensions(jpegHeader(1024, 576))
	if w != 1024 || h != 576 {
		t.Fatalf("JPEG 尺寸 = %dx%d, 期望 1024x576", w, h)
	}
}

func TestDimensionsInvalid(t *testing.T) {
	for _, data := range [][]byte{
		nil,
		{},
		[]byte("not an image"),
		{0xFF, 0xD8}, // 只有 SOI，没有 SOF
	} {
		if w, h := Dimensions(data); w != 0 || h != 0 {
			t.Fatalf("非法数据应返回 0x0，实际 %dx%d", w, h)
		}
	}
}

func TestIsSquareish(t *testing.T) {
	cases := []struct {
		name string
		w, h int
		want bool
	}{
		{"方形图标", 512, 512, true},
		{"竖版封面 2:3", 600, 900, false},
		{"横幅 16:9", 1024, 576, false},
		{"轻微非方形仍在容差内", 500, 520, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsSquareish(jpegHeader(c.w, c.h)); got != c.want {
				t.Fatalf("IsSquareish(%dx%d) = %v, 期望 %v", c.w, c.h, got, c.want)
			}
		})
	}
}

func TestSubStoreIsolatesFiles(t *testing.T) {
	dir := t.TempDir()
	root := NewStore(dir)
	banners := root.Sub("switch-banners")
	icons := root.Sub("switch-icons")

	icon := jpegHeader(512, 512)
	banner := jpegHeader(1024, 576)

	if err := icons.Save("TID", 1, icon); err != nil {
		t.Fatalf("保存图标失败: %v", err)
	}
	if err := banners.Save("TID", 1, banner); err != nil {
		t.Fatalf("保存横幅失败: %v", err)
	}

	// 根目录不应被污染
	if p := root.Find("TID", 1); p != "" {
		t.Fatalf("根目录不应存在文件，实际 %s", p)
	}
	// 两个子目录各自独立，互不覆盖
	iconPath := icons.Find("TID", 1)
	bannerPath := banners.Find("TID", 1)
	if iconPath == "" || bannerPath == "" {
		t.Fatalf("子目录文件缺失: icon=%q banner=%q", iconPath, bannerPath)
	}
	if iconPath == bannerPath {
		t.Fatalf("图标与横幅不应指向同一文件: %s", iconPath)
	}
}

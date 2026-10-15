package sw

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildPFS0 构造最小合法 PFS0 容器
func buildPFS0(names []string, sizes []int) []byte {
	count := len(names)
	strTable := []byte{}
	offs := make([]uint32, count)
	for i, n := range names {
		offs[i] = uint32(len(strTable))
		strTable = append(strTable, []byte(n)...)
		strTable = append(strTable, 0)
	}
	hdrSize := 0x10 + count*0x18 + len(strTable)
	buf := make([]byte, hdrSize)
	copy(buf[0:4], "PFS0")
	binary.LittleEndian.PutUint32(buf[4:8], uint32(count))
	binary.LittleEndian.PutUint32(buf[8:12], uint32(len(strTable)))
	// entries
	dataOff := 0
	for i := 0; i < count; i++ {
		e := buf[0x10+i*0x18 : 0x10+(i+1)*0x18]
		binary.LittleEndian.PutUint64(e[0:8], uint64(dataOff))
		binary.LittleEndian.PutUint64(e[8:16], uint64(sizes[i]))
		binary.LittleEndian.PutUint32(e[16:20], offs[i])
		dataOff += sizes[i]
	}
	copy(buf[0x10+count*0x18:], strTable)
	// data
	data := make([]byte, dataOff)
	buf = append(buf, data...)
	return buf
}

// buildHFS0 构造最小合法 HFS0（name->(offset,size)），dataBase 之后为数据
func buildHFS0(names []string, sizes []int) []byte {
	count := len(names)
	strTable := []byte{}
	offs := make([]uint32, count)
	for i, n := range names {
		offs[i] = uint32(len(strTable))
		strTable = append(strTable, []byte(n)...)
		strTable = append(strTable, 0)
	}
	hdrSize := 0x10 + count*0x40 + len(strTable)
	buf := make([]byte, hdrSize)
	copy(buf[0:4], "HFS0")
	binary.LittleEndian.PutUint32(buf[4:8], uint32(count))
	binary.LittleEndian.PutUint32(buf[8:12], uint32(len(strTable)))
	dataOff := 0
	for i := 0; i < count; i++ {
		e := buf[0x10+i*0x40 : 0x10+(i+1)*0x40]
		binary.LittleEndian.PutUint64(e[0:8], uint64(dataOff))
		binary.LittleEndian.PutUint64(e[8:16], uint64(sizes[i]))
		binary.LittleEndian.PutUint32(e[16:20], offs[i])
		dataOff += sizes[i]
	}
	copy(buf[0x10+count*0x40:], strTable)
	buf = append(buf, make([]byte, dataOff)...)
	return buf
}

func TestClassifyAndBase(t *testing.T) {
	cases := []struct{ id, wantCat, wantBase string }{
		{"0100F2C0115B6000", "game", "0100F2C0115B6000"},  // 王国之泪 本体
		{"0100F2C0115B6800", "patch", "0100F2C0115B6000"}, // 更新
		{"0100F2C0115B7001", "dlc", "0100F2C0115B6000"},   // DLC1
		{"0100F2C0115B7002", "dlc", "0100F2C0115B6000"},   // DLC2
		{"01007EF00011E000", "game", "01007EF00011E000"},  // 荒野之息 本体
		{"01007EF00011F001", "dlc", "01007EF00011E000"},   // 荒野之息 DLC
	}
	for _, c := range cases {
		if got := ClassifyTitleID(c.id); got != c.wantCat {
			t.Errorf("Classify(%s)=%s want %s", c.id, got, c.wantCat)
		}
		if got := BaseTitleID(c.id); got != c.wantBase {
			t.Errorf("Base(%s)=%s want %s", c.id, got, c.wantBase)
		}
	}
}

func TestFormatVersion(t *testing.T) {
	cases := map[int]string{0: "v1.0.0", 65536: "v1.0.0", 131072: "v2.0.0", 65537: "v1.0.1"}
	for in, want := range cases {
		if got := FormatVersion(in); got != want {
			t.Errorf("FormatVersion(%d)=%s want %s", in, got, want)
		}
	}
}

func TestParseNSP(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "The Legend of Zelda - Tears of the Kingdom [0100F2C0115B6000][v0].nsp")
	data := buildPFS0([]string{
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.cnmt.nca",
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb.nca",
	}, []int{512, 4096})
	if err := os.WriteFile(p, data, 0644); err != nil {
		t.Fatal(err)
	}
	info, err := Parse(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("TitleID=%s Raw=%s Cat=%s Ver=%s Title=%q Container=%s Files=%d",
		info.TitleID, info.RawTitleID, info.Category, info.Version, info.Title, info.Container, info.FileCount)
	if info.TitleID != "0100F2C0115B6000" {
		t.Errorf("TitleID=%s", info.TitleID)
	}
	if info.Category != "game" {
		t.Errorf("Category=%s want game", info.Category)
	}
	if info.Container != "NSP" || info.FileCount != 2 {
		t.Errorf("Container=%s Files=%d", info.Container, info.FileCount)
	}
	if !strings.Contains(info.Title, "Tears of the Kingdom") {
		t.Errorf("Title=%q", info.Title)
	}
}

func TestParseNSPDLC(t *testing.T) {
	dir := t.TempDir()
	// DLC 文件名 TitleID = 本体 + 0x1000 + 1
	p := filepath.Join(dir, "[0100F2C0115B7001][v0].nsp")
	os.WriteFile(p, buildPFS0([]string{"x.nca"}, []int{128}), 0644)
	info, _ := Parse(p)
	t.Logf("DLC: TitleID=%s Cat=%s", info.TitleID, info.Category)
	if info.Category != "dlc" {
		t.Errorf("Category=%s want dlc", info.Category)
	}
	if info.TitleID != "0100F2C0115B6000" {
		t.Errorf("归一化 TitleID=%s want 0100F2C0115B6000", info.TitleID)
	}
}

func TestParseXCI(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "Mario Kart 8 Deluxe [0100152000022000][v65536].xci")

	// secure 分区：3 个 NCA
	secure := buildHFS0([]string{"a.nca", "b.nca", "c.nca"}, []int{64, 64, 64})
	// 根分区：update + secure + logo
	root := buildHFS0([]string{"update", "secure", "logo"}, []int{32, len(secure), 32})

	// 布局：header(0x100) + 0x100 HEAD 区 ... 把 root 放在 0x10000，secure 紧随其后
	const rootOff = 0x10000
	secureOff := rootOff + len(root)
	buf := make([]byte, secureOff+len(secure))
	copy(buf[0x100:0x104], "HEAD")
	binary.LittleEndian.PutUint64(buf[0x130:0x138], uint64(rootOff))
	// 修正 root 中 secure 的偏移（相对 root 数据区）
	copy(buf[rootOff:], root)
	// 把 secure 放到 secureOff（root 数据区首条之后）
	copy(buf[secureOff:], secure)

	os.WriteFile(p, buf, 0644)
	info, err := Parse(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("XCI: TitleID=%s Cat=%s Ver=%s Title=%q Container=%s Files=%d",
		info.TitleID, info.Category, info.Version, info.Title, info.Container, info.FileCount)
	if info.Container != "XCI" {
		t.Errorf("Container=%s want XCI", info.Container)
	}
	if info.Version != "v1.0.0" || info.Category != "game" {
		t.Errorf("Ver=%s Cat=%s", info.Version, info.Category)
	}
	if !strings.Contains(info.Title, "Mario Kart") {
		t.Errorf("Title=%q", info.Title)
	}
}

func TestExtractIconDecrypted(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "Decrypted Game [0100152000022000][v0].nsp")
	// NACP 魔数 + 一段伪造 JPEG
	jpg := append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, make([]byte, 5000)...)
	jpg = append(jpg, 0xFF, 0xD9)
	payload := append([]byte("NACP"), make([]byte, 1000)...)
	payload = append(payload, jpg...)
	os.WriteFile(p, payload, 0644)

	icon := ExtractIcon(p)
	if len(icon) == 0 {
		t.Fatal("expected icon extracted")
	}
	t.Logf("extracted icon bytes=%d", len(icon))
	if icon[0] != 0xFF || icon[1] != 0xD8 {
		t.Error("not a jpeg")
	}
}

func TestExtractIconEncrypted(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "Encrypted [0100152000022000][v0].nsp")
	os.WriteFile(p, make([]byte, 100000), 0644) // 全 0，无 NACP
	if icon := ExtractIcon(p); len(icon) != 0 {
		t.Errorf("expected nil for encrypted, got %d bytes", len(icon))
	}
}

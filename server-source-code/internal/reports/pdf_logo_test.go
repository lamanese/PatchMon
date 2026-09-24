package reports

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func testImage(t *testing.T, w, h int, encode func(*bytes.Buffer, image.Image)) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		img.Set(x, 0, color.RGBA{R: 255, A: 255})
	}
	var buf bytes.Buffer
	encode(&buf, img)
	return buf.Bytes()
}

func pngBytes(t *testing.T, w, h int) []byte {
	return testImage(t, w, h, func(b *bytes.Buffer, i image.Image) { _ = png.Encode(b, i) })
}

func jpegBytes(t *testing.T, w, h int) []byte {
	return testImage(t, w, h, func(b *bytes.Buffer, i image.Image) { _ = jpeg.Encode(b, i, nil) })
}

// assert8BitPNG fails unless b is a PNG that decodes as 8-bit (N)RGBA.
func assert8BitPNG(t *testing.T, b []byte) image.Config {
	t.Helper()
	cfg, err := png.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("output is not a PNG: %v", err)
	}
	if cfg.ColorModel != color.NRGBAModel && cfg.ColorModel != color.RGBAModel {
		t.Fatalf("output colour model is not 8-bit (N)RGBA: %T", cfg.ColorModel)
	}
	if _, err := png.Decode(bytes.NewReader(b)); err != nil {
		t.Fatalf("output does not decode: %v", err)
	}
	return cfg
}

func TestSelectLogoReencodesUploadedPNG(t *testing.T) {
	// stored uncompressed, so a pass-through would be byte-identical while a
	// re-encode (default compression) is not
	raw := testImage(t, 300, 80, func(b *bytes.Buffer, i image.Image) {
		_ = (&png.Encoder{CompressionLevel: png.NoCompression}).Encode(b, i)
	})
	img, typ, src := selectLogo(Branding{LogoData: raw, LogoContentType: "image/png"})
	if src != LogoSourceUploaded || typ != "png" {
		t.Fatalf("got src=%s typ=%s", src, typ)
	}
	// the raw upload must never reach fpdf, not even when it is small
	if bytes.Equal(img, raw) {
		t.Fatal("uploaded bytes passed through without re-encoding")
	}
	if cfg := assert8BitPNG(t, img); cfg.Width != 300 || cfg.Height != 80 {
		t.Fatalf("size changed: %dx%d", cfg.Width, cfg.Height)
	}
}

func TestSelectLogoConvertsUploadedJPEGToPNG(t *testing.T) {
	img, typ, src := selectLogo(Branding{LogoData: jpegBytes(t, 200, 50), LogoContentType: "Image/JPEG; charset=binary"})
	if src != LogoSourceUploaded || typ != "png" {
		t.Fatalf("got src=%s typ=%s", src, typ)
	}
	if cfg := assert8BitPNG(t, img); cfg.Width != 200 || cfg.Height != 50 {
		t.Fatalf("size changed: %dx%d", cfg.Width, cfg.Height)
	}
}

func TestSelectLogoTruncatedPNGFallsBackToDefault(t *testing.T) {
	raw := pngBytes(t, 400, 300)
	cut := raw[:len(raw)/2] // header intact, IDAT cut off
	if _, err := png.DecodeConfig(bytes.NewReader(cut)); err != nil {
		t.Fatalf("test setup: header must still parse: %v", err)
	}
	img, typ, src := selectLogo(Branding{LogoData: cut, LogoContentType: "image/png"})
	if src != LogoSourceDefault || typ != "png" || !bytes.Equal(img, defaultLogoPNG) {
		t.Fatalf("got src=%s typ=%s", src, typ)
	}
}

// interlacedPNG turns a valid PNG into one whose IHDR declares Adam7
// interlacing (Go cannot write interlaced PNGs). The pixel data no longer
// matches, which is fine: either the decoder rejects it (default logo) or
// it is re-encoded; the raw bytes must never pass through.
func interlacedPNG(t *testing.T, raw []byte) []byte {
	t.Helper()
	b := append([]byte(nil), raw...)
	// signature (8) + length (4) + "IHDR" (4) + 13 data bytes + CRC (4)
	if string(b[12:16]) != "IHDR" {
		t.Fatal("test setup: IHDR not first")
	}
	b[16+12] = 1 // interlace method
	binary.BigEndian.PutUint32(b[29:33], crc32.ChecksumIEEE(b[12:29]))
	return b
}

func TestSelectLogoNeverPassesInterlacedPNGThrough(t *testing.T) {
	raw := interlacedPNG(t, pngBytes(t, 64, 32))
	img, typ, src := selectLogo(Branding{LogoData: raw, LogoContentType: "image/png"})
	if typ != "png" || bytes.Equal(img, raw) {
		t.Fatalf("interlaced PNG passed through (src=%s typ=%s)", src, typ)
	}
	if src == LogoSourceUploaded {
		assert8BitPNG(t, img)
	} else if !bytes.Equal(img, defaultLogoPNG) {
		t.Fatalf("src=%s but not the default logo", src)
	}
	// and a real interlaced header check: the output is never interlaced
	if img[28] != 0 {
		t.Fatal("output PNG is interlaced")
	}
}

func TestSelectLogo16BitPNGIsReducedTo8Bit(t *testing.T) {
	src16 := image.NewRGBA64(image.Rect(0, 0, 120, 40))
	for x := 0; x < 120; x++ {
		src16.Set(x, 0, color.RGBA64{R: 0xffff, A: 0xffff})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src16); err != nil {
		t.Fatal(err)
	}
	if cfg, _ := png.DecodeConfig(bytes.NewReader(buf.Bytes())); cfg.ColorModel != color.RGBA64Model && cfg.ColorModel != color.NRGBA64Model {
		t.Fatalf("test setup: input is not 16-bit: %T", cfg.ColorModel)
	}
	img, typ, src := selectLogo(Branding{LogoData: buf.Bytes(), LogoContentType: "image/png"})
	if src != LogoSourceUploaded || typ != "png" {
		t.Fatalf("got src=%s typ=%s", src, typ)
	}
	assert8BitPNG(t, img)
}

func TestSelectLogoPixelLimitBoundary(t *testing.T) {
	// 2000x2000 = exactly MaxLogoPixels: accepted (and scaled to 600 wide)
	img, _, src := selectLogo(Branding{LogoData: pngBytes(t, 2000, 2000), LogoContentType: "image/png"})
	if src != LogoSourceUploaded {
		t.Fatalf("2000x2000: got src=%s", src)
	}
	if cfg := assert8BitPNG(t, img); cfg.Width != 600 || cfg.Height != 600 {
		t.Fatalf("2000x2000 scaled to %dx%d", cfg.Width, cfg.Height)
	}
	// one column more: rejected on the header, before any decode
	if _, _, src := selectLogo(Branding{LogoData: pngBytes(t, 2001, 2000), LogoContentType: "image/png"}); src != LogoSourceDefault {
		t.Fatalf("2001x2000: got src=%s", src)
	}
}

func TestSelectLogoScalesWideImagesTo600(t *testing.T) {
	img, typ, src := selectLogo(Branding{LogoData: pngBytes(t, 1500, 300), LogoContentType: "image/png"})
	if src != LogoSourceUploaded || typ != "png" {
		t.Fatalf("got src=%s typ=%s", src, typ)
	}
	if cfg := assert8BitPNG(t, img); cfg.Width != 600 || cfg.Height != 120 {
		t.Fatalf("scaled size: %dx%d", cfg.Width, cfg.Height)
	}
}

func TestSelectLogoFallsBackToDefault(t *testing.T) {
	cases := map[string]Branding{
		"none":       {},
		"svg":        {LogoData: []byte("<svg xmlns='http://www.w3.org/2000/svg'/>"), LogoContentType: "image/svg+xml"},
		"corrupt":    {LogoData: []byte("not an image"), LogoContentType: "image/png"},
		"mismatch":   {LogoData: jpegBytes(t, 10, 10), LogoContentType: "image/png"},
		"too large":  {LogoData: pngBytes(t, 2100, 2000), LogoContentType: "image/png"},
		"type empty": {LogoData: pngBytes(t, 10, 10)},
	}
	for name, b := range cases {
		img, typ, src := selectLogo(b)
		if src != LogoSourceDefault || typ != "png" || !bytes.Equal(img, defaultLogoPNG) {
			t.Errorf("%s: want default logo, got src=%s typ=%s", name, src, typ)
		}
	}
}

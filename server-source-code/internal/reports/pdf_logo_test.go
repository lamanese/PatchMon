package reports

import (
	"bytes"
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

func TestSelectLogoUsesUploadedPNG(t *testing.T) {
	raw := pngBytes(t, 300, 80)
	img, typ, src := selectLogo(Branding{LogoData: raw, LogoContentType: "image/png"})
	if src != LogoSourceUploaded || typ != "png" || !bytes.Equal(img, raw) {
		t.Fatalf("got src=%s typ=%s same=%v", src, typ, bytes.Equal(img, raw))
	}
}

func TestSelectLogoUsesUploadedJPEGWithParameters(t *testing.T) {
	_, typ, src := selectLogo(Branding{LogoData: jpegBytes(t, 200, 50), LogoContentType: "Image/JPEG; charset=binary"})
	if src != LogoSourceUploaded || typ != "jpg" {
		t.Fatalf("got src=%s typ=%s", src, typ)
	}
}

func TestSelectLogoScalesWideImagesTo600(t *testing.T) {
	img, typ, src := selectLogo(Branding{LogoData: pngBytes(t, 1500, 300), LogoContentType: "image/png"})
	if src != LogoSourceUploaded || typ != "png" {
		t.Fatalf("got src=%s typ=%s", src, typ)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(img))
	if err != nil || cfg.Width != 600 || cfg.Height != 120 {
		t.Fatalf("scaled size: %dx%d err=%v", cfg.Width, cfg.Height, err)
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

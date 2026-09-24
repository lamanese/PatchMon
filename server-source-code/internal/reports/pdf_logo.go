package reports

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"mime"
	"strings"

	"golang.org/x/image/draw"
)

const (
	LogoSourceUploaded = "uploaded"
	LogoSourceDefault  = "default"
	MaxLogoPixels      = 4_000_000
	MaxLogoWidth       = 600
)

// selectLogo picks the header logo for a PDF: the uploaded light logo when it
// is a sane PNG/JPEG, otherwise the embedded vendor default. It never fails.
func selectLogo(b Branding) ([]byte, string, string) {
	if len(b.LogoData) == 0 {
		return defaultLogoPNG, "png", LogoSourceDefault
	}
	mt, _, err := mime.ParseMediaType(strings.ToLower(strings.TrimSpace(b.LogoContentType)))
	if err != nil {
		return defaultLogoPNG, "png", LogoSourceDefault
	}
	var want string
	switch mt {
	case "image/png":
		want = "png"
	case "image/jpeg", "image/jpg":
		want = "jpeg"
	default:
		return defaultLogoPNG, "png", LogoSourceDefault
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(b.LogoData))
	if err != nil || format != want || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > MaxLogoPixels {
		return defaultLogoPNG, "png", LogoSourceDefault
	}
	typ := "png"
	if want == "jpeg" {
		typ = "jpg"
	}
	if cfg.Width <= MaxLogoWidth {
		return b.LogoData, typ, LogoSourceUploaded
	}
	src, _, err := image.Decode(bytes.NewReader(b.LogoData))
	if err != nil {
		return defaultLogoPNG, "png", LogoSourceDefault
	}
	h := cfg.Height * MaxLogoWidth / cfg.Width
	if h < 1 {
		h = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, MaxLogoWidth, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	var out bytes.Buffer
	if err := png.Encode(&out, dst); err != nil {
		return defaultLogoPNG, "png", LogoSourceDefault
	}
	return out.Bytes(), "png", LogoSourceUploaded
}

// keep the decoders linked for image.DecodeConfig
var _ = jpeg.Decode

package mediaformat

import (
	"bytes"
	"image"
	_ "image/gif"  // registers the GIF config decoder
	_ "image/jpeg" // registers the JPEG config decoder
	_ "image/png"  // registers the PNG config decoder

	_ "golang.org/x/image/webp" // registers the WebP config decoder
)

// MaxPixels bounds how large an image the server decodes in full. A small
// file can declare enormous dimensions, so callers check the header first.
const MaxPixels = 40_000_000

// Dimensions reads only the image header. AVIF has no decoder here and
// reports false, as does any header that disagrees with the sniffed type.
func Dimensions(content []byte) (int, int, bool) {
	mime := Sniff(content)
	if mime == "" || mime == "image/avif" {
		return 0, 0, false
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil || "image/"+format != mime || config.Width <= 0 || config.Height <= 0 {
		return 0, 0, false
	}
	return config.Width, config.Height, true
}

package mediaformat

func Sniff(content []byte) string {
	if len(content) >= 3 && content[0] == 0xff && content[1] == 0xd8 && content[2] == 0xff {
		return "image/jpeg"
	}
	if len(content) >= 8 && string(content[:8]) == "\x89PNG\r\n\x1a\n" {
		return "image/png"
	}
	if len(content) >= 6 && (string(content[:6]) == "GIF87a" || string(content[:6]) == "GIF89a") {
		return "image/gif"
	}
	if len(content) >= 12 && string(content[:4]) == "RIFF" && string(content[8:12]) == "WEBP" {
		return "image/webp"
	}
	if len(content) >= 12 && string(content[4:8]) == "ftyp" && (string(content[8:12]) == "avif" || string(content[8:12]) == "avis") {
		return "image/avif"
	}
	return ""
}
func Extension(mime string) string {
	return map[string]string{"image/jpeg": "jpg", "image/png": "png", "image/gif": "gif", "image/webp": "webp", "image/avif": "avif"}[mime]
}

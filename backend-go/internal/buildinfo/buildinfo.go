// Package buildinfo holds values that release builds inject at link time.
package buildinfo

// Version is the application version from package.json, without the tag's
// "v" prefix. Builds set it with
// -ldflags "-X github.com/yexca/kano-navi/backend-go/internal/buildinfo.Version=<version>";
// a plain go build or go test keeps the development value.
var Version = "0.0.0-dev"

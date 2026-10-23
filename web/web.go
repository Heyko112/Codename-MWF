// Package web bettet Templates und statische Dateien ins Binary ein.
package web

import "embed"

//go:embed templates static
var FS embed.FS

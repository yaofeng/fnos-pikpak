// Package webdist holds the built frontend. `npm run build` fills dist/; the
// committed dist/.keep keeps a fresh clone buildable, and fallback.html stands
// in for the SPA until something builds it.
package webdist

import "embed"

//go:embed all:dist
var FS embed.FS

//go:embed fallback.html
var Fallback []byte

//go:build !dev

// Package web serves production assets or the isolated development proxy.
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:dist
var productionAssets embed.FS

const Development = false

func Handler(_ string) (http.Handler, error) {
	assets, err := fs.Sub(productionAssets, "dist")
	if err != nil {
		return nil, err
	}
	return http.FileServer(http.FS(assets)), nil
}

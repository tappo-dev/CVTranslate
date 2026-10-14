//go:build dev

package web

import (
	"errors"
	"net/http"
	"net/http/httputil"
	"net/url"
)

const Development = true

func Handler(viteURL string) (http.Handler, error) {
	target, err := url.Parse(viteURL)
	if err != nil || target.Scheme != "http" || target.Hostname() != "127.0.0.1" || target.User != nil || target.RawQuery != "" || target.Fragment != "" {
		return nil, errors.New("Vite must use a loopback HTTP URL")
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(writer http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(writer, "Development frontend unavailable. Start npm run dev or restart F5.", http.StatusBadGateway)
	}
	return proxy, nil
}

package transportspike

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// NewApplicationHandler serves the framework-generated boot shell, Go
// toolchain runtime, WASM client, and embedded gRPC bridge from one origin.
func NewApplicationHandler(assetsDirectory string, launchSecret string) (http.Handler, *Service, error) {
	assetsDirectory, err := filepath.Abs(assetsDirectory)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve assets directory: %w", err)
	}
	required := map[string]string{
		"/":              "index.html",
		"/wasm_exec.js":  "wasm_exec.js",
		"/bin/main.wasm": filepath.Join("bin", "main.wasm"),
	}
	for _, relative := range required {
		info, statErr := os.Stat(filepath.Join(assetsDirectory, relative))
		if statErr != nil {
			return nil, nil, fmt.Errorf("inspect generated asset %s: %w", relative, statErr)
		}
		if !info.Mode().IsRegular() {
			return nil, nil, fmt.Errorf("generated asset %s is not a regular file", relative)
		}
	}

	service := &Service{}
	bridge, err := NewHandler(service, launchSecret)
	if err != nil {
		return nil, nil, err
	}

	mux := http.NewServeMux()
	mux.Handle("/grpc", bridge)
	for route, relative := range required {
		if route == "/" {
			continue
		}
		mux.Handle(route, generatedAssetHandler(filepath.Join(assetsDirectory, relative)))
	}
	shellPath := filepath.Join(assetsDirectory, "index.html")
	mux.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if strings.Contains(filepath.Base(request.URL.Path), ".") {
			http.NotFound(writer, request)
			return
		}
		applyBrowserSecurityHeaders(writer.Header())
		http.SetCookie(writer, SessionCookie(launchSecret))
		http.ServeFile(writer, request, shellPath)
	})
	return mux, service, nil
}

func generatedAssetHandler(path string) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		applyBrowserSecurityHeaders(writer.Header())
		writer.Header().Set("Cache-Control", "no-store")
		switch filepath.Ext(path) {
		case ".js":
			writer.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		case ".wasm":
			writer.Header().Set("Content-Type", "application/wasm")
		}
		file, err := os.Open(path)
		if err != nil {
			http.NotFound(writer, request)
			return
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			http.Error(writer, "inspect generated asset", http.StatusInternalServerError)
			return
		}
		http.ServeContent(writer, request, info.Name(), info.ModTime(), file)
	})
}

func applyBrowserSecurityHeaders(header http.Header) {
	header.Set("Cache-Control", "no-store")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("Cross-Origin-Opener-Policy", "same-origin")
	header.Set(
		"Content-Security-Policy",
		"default-src 'self'; connect-src 'self' ws:; img-src 'self' data:; "+
			"script-src 'self' 'unsafe-inline' 'wasm-unsafe-eval'; style-src 'self' 'unsafe-inline'; "+
			"object-src 'none'; base-uri 'none'; frame-ancestors 'none'",
	)
}

// NewHTTPServer returns the bounded HTTP server used by the local spike.
func NewHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
}

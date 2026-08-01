// Package frontendserver serves the generated GWC application and its
// authenticated same-origin GoGRPCBridge transport.
package frontendserver

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"github.com/monstercameron/GoGRPCBridge/pkg/grpctunnel"
	"google.golang.org/grpc"
)

const (
	sessionCookieName = "codeflux_local_session"
	bridgePath        = "/grpc"
)

// Bootstrap is the secret-free compatibility envelope consumed before the
// browser opens a bridge session.
type Bootstrap struct {
	ApplicationVersion  string                     `json:"application_version"`
	APIVersion          string                     `json:"api_version"`
	SchemaVersion       int                        `json:"schema_version"`
	FrontendVersion     string                     `json:"frontend_version"`
	BridgePath          string                     `json:"bridge_path"`
	SelectedSessionID   *codefluxv1.StableIdentity `json:"selected_session_id,omitempty"`
	SelectedWorkspaceID *codefluxv1.StableIdentity `json:"selected_workspace_id,omitempty"`
	RouteAccess         RouteAccess                `json:"route_access"`
}

// RouteAccess is the minimal server-confirmed allowlist used to validate a
// browser-stored route. It intentionally contains stable identities only.
type RouteAccess struct {
	FirstRunComplete       bool                         `json:"first_run_complete"`
	AccessibleRepositories []*codefluxv1.StableIdentity `json:"accessible_repositories,omitempty"`
	AccessibleThreads      []*codefluxv1.StableIdentity `json:"accessible_threads,omitempty"`
	ArchivedThreads        []*codefluxv1.StableIdentity `json:"archived_threads,omitempty"`
}

// AssetSource supplies one browser asset by its slash-separated relative path.
//
// It exists so a released executable can serve assets compiled into itself
// while a development checkout serves the same files from a build directory,
// without this package needing to know which happened. web/assets.Resolved
// satisfies it structurally, so nothing here depends on that package.
type AssetSource interface {
	Get(relative string) ([]byte, error)
}

// Options binds generated assets and a product gRPC server to one local HTTP
// origin. SessionToken is retained only by the native process and an HttpOnly
// same-site cookie; it is never returned in bootstrap data.
//
// Exactly one of Assets and AssetsDirectory must be supplied. Accepting both
// would leave the precedence between an embedded release build and somebody's
// working tree to be discovered later, which is the kind of ambiguity that
// ships stale files.
type Options struct {
	// Assets is the resolved asset set, preferred when present.
	Assets AssetSource
	// AssetsDirectory reads the assets from disk.
	AssetsDirectory string
	GRPCServer      *grpc.Server
	SessionToken    string
	Bootstrap       Bootstrap
}

// NewHandler builds the production local frontend boundary.
func NewHandler(options Options) (http.Handler, error) {
	switch {
	case options.Assets != nil && options.AssetsDirectory != "":
		return nil, errors.New(
			"frontend assets were supplied both resolved and as a directory; supply one, " +
				"so which files are served is never a matter of precedence")
	case options.Assets == nil && options.AssetsDirectory == "":
		return nil, errors.New("frontend assets are required")
	case options.Assets == nil && !filepath.IsAbs(options.AssetsDirectory):
		return nil, errors.New("absolute frontend assets directory is required")
	}
	if options.GRPCServer == nil {
		return nil, errors.New("frontend gRPC server is required")
	}
	if len(options.SessionToken) < 32 {
		return nil, errors.New("frontend session token is too short")
	}
	if selected := options.Bootstrap.SelectedSessionID; selected != nil {
		if selected.GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_SESSION {
			return nil, errors.New("frontend selected session identity is invalid")
		}
		if _, parseErr := domain.ParseSessionID(selected.GetValue()); parseErr != nil {
			return nil, errors.New("frontend selected session identity is invalid")
		}
	}
	if selected := options.Bootstrap.SelectedWorkspaceID; selected != nil {
		if selected.GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_WORKSPACE {
			return nil, errors.New("frontend selected workspace identity is invalid")
		}
		if _, parseErr := domain.ParseWorkspaceID(selected.GetValue()); parseErr != nil {
			return nil, errors.New("frontend selected workspace identity is invalid")
		}
	}
	if err := validateRouteAccess(options.Bootstrap.RouteAccess); err != nil {
		return nil, err
	}
	source := options.Assets
	if source == nil {
		source = directoryAssets(options.AssetsDirectory)
	}
	assets, err := loadAssets(source)
	if err != nil {
		return nil, err
	}
	options.Bootstrap.BridgePath = bridgePath
	bootstrap, err := json.Marshal(options.Bootstrap)
	if err != nil {
		return nil, fmt.Errorf("marshal frontend bootstrap: %w", err)
	}

	mux := http.NewServeMux()
	bridge := grpctunnel.Wrap(
		options.GRPCServer,
		grpctunnel.WithOriginCheck(sameOrigin),
		grpctunnel.WithAuthorize(authorizeCookie(options.SessionToken)),
		grpctunnel.WithReadLimitBytes(4<<20),
		grpctunnel.WithMaxActiveConnections(32),
		grpctunnel.WithMaxConnectionsPerClient(4),
		grpctunnel.WithMaxUpgradesPerClientPerMinute(60),
		grpctunnel.WithSessionMaxLifetime(30*time.Minute),
	)
	mux.Handle("GET "+bridgePath, withBridgeRequestIdentity(bridge))
	mux.HandleFunc("GET /bootstrap", func(writer http.ResponseWriter, request *http.Request) {
		setSessionCookie(writer, options.SessionToken)
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("Cache-Control", "no-store")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write(bootstrap)
	})
	mux.HandleFunc("GET /wasm_exec.js", serveAsset(assets.wasmShim, "text/javascript; charset=utf-8"))
	mux.HandleFunc("GET /bin/main.wasm", serveAsset(assets.wasmBinary, "application/wasm"))
	mux.HandleFunc("GET /", func(writer http.ResponseWriter, request *http.Request) {
		if !isApplicationRoute(request.URL.Path) {
			http.NotFound(writer, request)
			return
		}
		setSessionCookie(writer, options.SessionToken)
		serveBytes(writer, assets.index, "text/html; charset=utf-8")
	})
	return securityHeaders(loopbackOnly(mux), assets.inlineScriptSources), nil
}

// withBridgeRequestIdentity creates the trusted request identity that the
// product gRPC boundary accepts after the WebSocket handler has independently
// authenticated the HttpOnly launch cookie. Browser WebSocket APIs cannot set
// arbitrary handshake headers, so this identity must be server-generated and
// must overwrite any client-supplied value.
func withBridgeRequestIdentity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var entropy [16]byte
		if _, err := rand.Read(entropy[:]); err != nil {
			http.Error(writer, "local bridge identity unavailable", http.StatusServiceUnavailable)
			return
		}
		forward := request.Clone(request.Context())
		forward.Header = request.Header.Clone()
		forward.Header.Set("X-Request-Id", "bridge-"+hex.EncodeToString(entropy[:]))
		next.ServeHTTP(writer, forward)
	})
}

func validateRouteAccess(access RouteAccess) error {
	for _, identity := range access.AccessibleRepositories {
		if identity == nil || identity.GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_REPOSITORY {
			return errors.New("frontend route access repository identity is invalid")
		}
		if _, err := domain.ParseRepositoryID(identity.GetValue()); err != nil {
			return errors.New("frontend route access repository identity is invalid")
		}
	}
	for _, identities := range [][]*codefluxv1.StableIdentity{
		access.AccessibleThreads,
		access.ArchivedThreads,
	} {
		for _, identity := range identities {
			if identity == nil || identity.GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_THREAD {
				return errors.New("frontend route access thread identity is invalid")
			}
			if _, err := domain.ParseThreadID(identity.GetValue()); err != nil {
				return errors.New("frontend route access thread identity is invalid")
			}
		}
	}
	return nil
}

type generatedAssets struct {
	index               []byte
	wasmShim            []byte
	wasmBinary          []byte
	inlineScriptSources string
}

// directoryAssets reads the generated assets from a build directory.
type directoryAssets string

func (directory directoryAssets) Get(relative string) ([]byte, error) {
	path := filepath.Join(string(directory), filepath.FromSlash(relative))
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read generated frontend asset %s: %w", relative, err)
	}
	return content, nil
}

func loadAssets(source AssetSource) (generatedAssets, error) {
	load := source.Get
	index, err := load("index.html")
	if err != nil {
		return generatedAssets{}, err
	}
	index = normalizeGeneratedIndex(index)
	shim, err := load("wasm_exec.js")
	if err != nil {
		return generatedAssets{}, err
	}
	binary, err := load("bin/main.wasm")
	if err != nil {
		return generatedAssets{}, err
	}
	return generatedAssets{
		index: index, wasmShim: shim, wasmBinary: binary,
		inlineScriptSources: inlineScriptSources(index),
	}, nil
}

// normalizeGeneratedIndex keeps the framework-owned document intact while
// making its generated runtime references safe for history-router deep links.
// GWC scaffolds relative URLs because its standalone development server mounts
// at root; CodeFlux also serves that document from canonical nested routes.
func normalizeGeneratedIndex(index []byte) []byte {
	index = bytes.ReplaceAll(index, []byte(`src="./wasm_exec.js"`), []byte(`src="/wasm_exec.js"`))
	index = bytes.ReplaceAll(index, []byte(`"./bin/main.wasm"`), []byte(`"/bin/main.wasm"`))
	index = bytes.ReplaceAll(index, []byte(`'./bin/main.wasm'`), []byte(`'/bin/main.wasm'`))
	return index
}

func serveAsset(content []byte, contentType string) http.HandlerFunc {
	return func(writer http.ResponseWriter, _ *http.Request) {
		serveBytes(writer, content, contentType)
	}
}

func serveBytes(writer http.ResponseWriter, content []byte, contentType string) {
	writer.Header().Set("Content-Type", contentType)
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Length", fmt.Sprint(len(content)))
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(content)
}

func isApplicationRoute(path string) bool {
	if path == "/" {
		return true
	}
	for _, prefix := range []string{
		"/tasks",
		"/workspace",
		"/memory",
		"/settings",
		"/diagnostics",
		"/first-run",
	} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

func setSessionCookie(writer http.ResponseWriter, token string) {
	http.SetCookie(writer, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   0,
	})
}

func authorizeCookie(token string) func(*http.Request) error {
	return func(request *http.Request) error {
		cookie, err := request.Cookie(sessionCookieName)
		if err != nil || len(cookie.Value) != len(token) ||
			subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(token)) != 1 {
			return errors.New("valid local session cookie is required")
		}
		return nil
	}
}

func sameOrigin(request *http.Request) bool {
	origin := strings.TrimSpace(request.Header.Get("Origin"))
	if origin == "" || !isLoopbackHost(request.Host) {
		return false
	}
	return origin == "http://"+request.Host || origin == "https://"+request.Host
}

func loopbackOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !isLoopbackHost(request.Host) {
			http.Error(writer, "local loopback host required", http.StatusMisdirectedRequest)
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func isLoopbackHost(hostPort string) bool {
	host := strings.TrimSpace(hostPort)
	if host == "" {
		return false
	}
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		host = parsed
	}
	host = strings.Trim(host, "[]")
	// "localhost" is accepted because internal/config accepts it as a bind
	// host, and a browser pointed at a supported configuration must not be
	// refused by its own server. It is the only name accepted: RFC 6761
	// reserves it for loopback and forbids resolvers from sending it to DNS,
	// so unlike an ordinary hostname it cannot be rebound to another address.
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

func inlineScriptSources(index []byte) string {
	const (
		openScript  = "<script"
		closeScript = "</script>"
	)
	var sources []string
	remaining := index
	for {
		open := bytes.Index(bytes.ToLower(remaining), []byte(openScript))
		if open < 0 {
			break
		}
		remaining = remaining[open:]
		tagEnd := bytes.IndexByte(remaining, '>')
		if tagEnd < 0 {
			break
		}
		tag := bytes.ToLower(remaining[:tagEnd+1])
		remaining = remaining[tagEnd+1:]
		close := bytes.Index(bytes.ToLower(remaining), []byte(closeScript))
		if close < 0 {
			break
		}
		body := remaining[:close]
		remaining = remaining[close+len(closeScript):]
		if bytes.Contains(tag, []byte(" src=")) {
			continue
		}
		sum := sha256.Sum256(body)
		sources = append(
			sources,
			"'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'",
		)
	}
	return strings.Join(sources, " ")
}

func securityHeaders(next http.Handler, inlineScriptSources string) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		scriptSources := "'self' 'wasm-unsafe-eval'"
		if inlineScriptSources != "" {
			scriptSources += " " + inlineScriptSources
		}
		connectSources := "'self'"
		if isLoopbackHost(request.Host) {
			connectSources += " ws://" + request.Host
		}
		writer.Header().Set(
			"Content-Security-Policy",
			"default-src 'self'; base-uri 'none'; form-action 'self'; "+
				"frame-ancestors 'none'; object-src 'none'; "+
				"script-src "+scriptSources+"; "+
				"style-src 'self' 'unsafe-inline'; "+
				"connect-src "+connectSources+"; img-src 'self' data:",
		)
		writer.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(writer, request)
	})
}

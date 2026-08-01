package devdiag

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"runtime"
	"sort"
	"strings"
)

// ProfileKind names one profile the development server can serve (M22-120).
type ProfileKind string

const (
	ProfileCPU       ProfileKind = "cpu"
	ProfileHeap      ProfileKind = "heap"
	ProfileGoroutine ProfileKind = "goroutine"
	ProfileMutex     ProfileKind = "mutex"
	ProfileBlock     ProfileKind = "block"
)

// AllProfileKinds returns every supported profile.
func AllProfileKinds() []ProfileKind {
	return []ProfileKind{
		ProfileCPU, ProfileHeap, ProfileGoroutine, ProfileMutex, ProfileBlock,
	}
}

// Valid reports whether a kind is supported.
func (kind ProfileKind) Valid() bool {
	for _, candidate := range AllProfileKinds() {
		if candidate == kind {
			return true
		}
	}
	return false
}

// Path returns the route this profile is served on.
func (kind ProfileKind) Path() string {
	if kind == ProfileCPU {
		return "/debug/pprof/profile"
	}
	return "/debug/pprof/" + string(kind)
}

// ProfilingOptions configures the profiling surface (M22-120).
type ProfilingOptions struct {
	// Enabled turns profiling on. Off by default: an always-on profiling
	// endpoint is a way to read a running agent's memory, and mutex and block
	// profiling additionally cost time on every contended operation.
	Enabled bool
	// Token authorises a request. It must be at least 32 bytes, matching the
	// browser session secret, so it cannot be guessed.
	Token string
	// MutexProfileFraction and BlockProfileRate are applied only while
	// profiling is enabled, and restored on shutdown.
	MutexProfileFraction int
	BlockProfileRate     int
}

// Validate rejects options that would expose or mis-tune the process.
func (options ProfilingOptions) Validate() error {
	if !options.Enabled {
		return nil
	}
	if len(options.Token) < 32 {
		return errors.New("profiling requires a token of at least 32 bytes")
	}
	if options.MutexProfileFraction < 0 {
		return errors.New("mutex profile fraction must not be negative")
	}
	if options.BlockProfileRate < 0 {
		return errors.New("block profile rate must not be negative")
	}
	return nil
}

// ErrProfilingDisabled is returned when profiling is requested while off.
var ErrProfilingDisabled = errors.New("profiling is disabled")

// Profiler serves authenticated loopback-only profiles (M22-120).
type Profiler struct {
	options       ProfilingOptions
	priorMutex    int
	priorBlock    int
	tuningApplied bool
}

// NewProfiler builds a profiler and applies its runtime tuning.
func NewProfiler(options ProfilingOptions) (*Profiler, error) {
	if err := options.Validate(); err != nil {
		return nil, err
	}
	profiler := &Profiler{options: options}
	if !options.Enabled {
		return profiler, nil
	}
	// runtime.SetMutexProfileFraction returns the previous value, so the
	// process can be restored to exactly what it was rather than to a guess.
	profiler.priorMutex = runtime.SetMutexProfileFraction(options.MutexProfileFraction)
	profiler.priorBlock = options.BlockProfileRate
	runtime.SetBlockProfileRate(options.BlockProfileRate)
	profiler.tuningApplied = true
	return profiler, nil
}

// Close restores the runtime tuning profiling changed.
func (profiler *Profiler) Close() {
	if profiler == nil || !profiler.tuningApplied {
		return
	}
	runtime.SetMutexProfileFraction(profiler.priorMutex)
	runtime.SetBlockProfileRate(0)
	profiler.tuningApplied = false
}

// Enabled reports whether profiling is on.
func (profiler *Profiler) Enabled() bool {
	return profiler != nil && profiler.options.Enabled
}

// Handler returns the profiling mux, already wrapped in the loopback and token
// checks (M22-120).
//
// When profiling is disabled it returns a handler that refuses everything,
// rather than nil: a caller that mounted nil would panic on the first request
// and learn about the mistake at the worst time.
func (profiler *Profiler) Handler() http.Handler {
	if !profiler.Enabled() {
		return http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			http.Error(writer, "profiling is disabled", http.StatusNotFound)
		})
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return profiler.requireLoopback(profiler.requireToken(mux))
}

// requireToken enforces the shared secret in constant time.
func (profiler *Profiler) requireToken(next http.Handler) http.Handler {
	token := []byte(profiler.options.Token)
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		presented := []byte(strings.TrimPrefix(
			request.Header.Get("Authorization"), "Bearer "))
		if len(presented) != len(token) ||
			subtle.ConstantTimeCompare(presented, token) != 1 {
			http.Error(writer, "profiling token required", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(writer, request)
	})
}

// requireLoopback refuses any request that did not arrive on loopback.
//
// The token alone is not enough: a profile is a dump of process memory, and it
// must not be reachable from another machine even by someone who obtained the
// token.
func (profiler *Profiler) requireLoopback(next http.Handler) http.Handler {
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
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

// ProfilePaths returns every served path, sorted, so a doctor command can list
// them without duplicating the routing table.
func (profiler *Profiler) ProfilePaths() []string {
	if !profiler.Enabled() {
		return nil
	}
	paths := make([]string, 0, len(AllProfileKinds()))
	for _, kind := range AllProfileKinds() {
		paths = append(paths, kind.Path())
	}
	sort.Strings(paths)
	return paths
}

// Describe returns a one-line summary of the profiling configuration, for a
// diagnostics report.
func (profiler *Profiler) Describe() string {
	if !profiler.Enabled() {
		return "profiling: disabled"
	}
	return fmt.Sprintf(
		"profiling: enabled on loopback with a token; mutex fraction %d, block rate %d",
		profiler.options.MutexProfileFraction, profiler.options.BlockProfileRate)
}

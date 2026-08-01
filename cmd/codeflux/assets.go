package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"codeflux.dev/codeflux/web/assets"
)

// DevelopmentAssetDirectory is where `codeflux-dev build-frontend` writes the
// browser assets, relative to the repository root.
//
// A development checkout embeds nothing, so start has to find the build
// directory to serve anything at all. Naming it here, once, is what keeps the
// build command and the start command from disagreeing about the path.
const DevelopmentAssetDirectory = ".artifacts/frontend"

// assetOrigin describes where a resolved asset set came from, so start can say
// it plainly rather than leaving a user to wonder which files are being served.
type assetOrigin string

const (
	originEmbedded    assetOrigin = "compiled into this executable"
	originFlag        assetOrigin = "the directory given by --assets"
	originEnvironment assetOrigin = "the directory named by CODEFLUX_ASSETS"
	originDevelopment assetOrigin = "the development build directory"
)

// ErrNoFrontendAssets reports that no asset set could be found.
var ErrNoFrontendAssets = errors.New("no browser assets are available")

// resolveFrontendAssets finds the browser assets for this process.
//
// The order is explicit-then-embedded-then-development. An explicitly named
// directory wins because a person who typed a path meant it; embedded comes
// next so a released executable never reads somebody's working tree; the
// development directory is last and only when it exists, so a checkout works
// with no flags and a release is never affected by one lying around.
func resolveFrontendAssets(
	flagDirectory string,
	lookupEnvironment func(string) (string, bool),
	workingDirectory string,
) (assets.Resolved, assetOrigin, error) {
	if directory := strings.TrimSpace(flagDirectory); directory != "" {
		return resolveAssetDirectory(directory, originFlag)
	}
	if lookupEnvironment != nil {
		if value, present := lookupEnvironment("CODEFLUX_ASSETS"); present &&
			strings.TrimSpace(value) != "" {
			return resolveAssetDirectory(strings.TrimSpace(value), originEnvironment)
		}
	}
	if assets.Embedded() {
		resolved, err := assets.Resolve("")
		if err != nil {
			return assets.Resolved{}, "", err
		}
		return resolved, originEmbedded, nil
	}
	if directory, found := developmentAssetDirectory(workingDirectory); found {
		return resolveAssetDirectory(directory, originDevelopment)
	}
	return assets.Resolved{}, "", fmt.Errorf(
		"%w: this is a development build, so nothing is compiled in. Build them with "+
			"`go run ./cmd/codeflux-dev build-frontend`, or point --assets at a directory "+
			"holding %s",
		ErrNoFrontendAssets, strings.Join(assets.RequiredAssets(), ", "))
}

// resolveAssetDirectory turns a user-supplied path into a resolved asset set.
//
// assets.Resolve prefers embedded assets, which is what keeps a released
// executable from reading a working tree. That means a --assets path can be
// overridden, so the reported origin comes from what was actually resolved
// rather than from what was asked for: telling somebody their flag was used
// when it was not is worse than ignoring it.
func resolveAssetDirectory(directory string, requested assetOrigin) (
	assets.Resolved, assetOrigin, error,
) {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return assets.Resolved{}, "", fmt.Errorf(
			"resolve assets directory %q: %w", directory, err)
	}
	resolved, err := assets.Resolve(absolute)
	if err != nil {
		return assets.Resolved{}, "", err
	}
	if resolved.Source == assets.SourceEmbedded {
		return resolved, originEmbedded, nil
	}
	return resolved, requested, nil
}

// developmentAssetDirectory finds the build directory in a checkout.
//
// It walks up from the working directory looking for the module root, so
// `codeflux start` works from anywhere inside the repository rather than only
// from its top level.
func developmentAssetDirectory(workingDirectory string) (string, bool) {
	current := workingDirectory
	if current == "" {
		return "", false
	}
	for {
		if info, err := os.Stat(filepath.Join(current, "go.mod")); err == nil && !info.IsDir() {
			candidate := filepath.Join(current, filepath.FromSlash(DevelopmentAssetDirectory))
			if directoryHasRequiredAssets(candidate) {
				return candidate, true
			}
			return "", false
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}
		current = parent
	}
}

// directoryHasRequiredAssets reports whether a directory holds a usable set.
//
// A partially built directory is treated as absent so the error names the
// build command, rather than reporting a missing file from a path the user
// never mentioned.
func directoryHasRequiredAssets(directory string) bool {
	for _, relative := range assets.RequiredAssets() {
		info, err := os.Stat(filepath.Join(directory, filepath.FromSlash(relative)))
		if err != nil || info.IsDir() || info.Size() == 0 {
			return false
		}
	}
	return true
}

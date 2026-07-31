//go:build !js || !wasm

package main

// main is a host-build sentinel. The production browser entry point is the
// js/wasm implementation in main.go; keeping a no-op native entry lets the
// repository-wide cross-platform build gate type-check the shared client code.
func main() {}

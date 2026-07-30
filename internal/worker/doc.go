// Package worker owns the bounded subprocess lifecycle for one active task.
// Workers receive scoped capabilities and credential-free commands from the
// coordinator and depend on inward domain contracts.
package worker

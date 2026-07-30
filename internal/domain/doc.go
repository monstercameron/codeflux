// Package domain defines Codeflux typed values, stable identities, states,
// transitions, errors, and implementation-neutral ports.
//
// Domain is the dependency center. It must not import SQLite, provider, Git,
// process, transport, browser, graph-rendering, or frontend implementations.
package domain

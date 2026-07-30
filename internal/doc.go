// Package internal contains Codeflux implementation packages.
//
// Dependencies point inward. Domain owns pure values, state transitions, and
// ports, and imports no adapter package. Application and coordinator packages
// compose domain operations through those ports. Storage, events, providers,
// workspace, Git, execution, worker, graph, review, and transport packages are
// adapters or bounded services and may depend on domain contracts, but domain
// never depends on them. Transport handlers and commands assemble dependencies;
// they do not own hidden policy. No backend package depends on web presentation.
package internal

package atomname

// SchemaVersion is the only currently supported atom-naming schema version
// (M21-146; AGENTS.md "Atom Naming Style"; docs/plan.md §7 "Atom Naming and
// Retrieval Identity"). It is defined independently from
// atomdoc.SchemaVersion (the atom-documentation schema) and from the
// separate, not-yet-implemented atom embedding-input schema (M21-127): the
// three are distinct version numbers that must never be conflated or reused
// for one another, even though today each of the two implemented ones
// happens to equal 1.
const SchemaVersion uint32 = 1

// Explicit bounds on naming inputs. Codeflux never parses, hashes, or
// compares an unbounded amount of naming text. None of these bounds impose a
// short semantic length limit on canonical names (M21-147 explicitly
// forbids that); they exist only to keep every input finite before it is
// validated.
const (
	// MaximumCanonicalNameBytes generously bounds a canonical Go identifier.
	// AGENTS.md prefers a long, precise name over a short generic one, so
	// this bound is sized to comfortably exceed any realistic
	// <Verb><DomainObject><ImportantQualifier><ObservableOutcome> phrase
	// while still rejecting unbounded input.
	MaximumCanonicalNameBytes = 256
	// MaximumRationaleFieldBytes bounds each field of a NamingRationale.
	MaximumRationaleFieldBytes = 2048
	// MaximumAliasTextBytes bounds one alias's raw text.
	MaximumAliasTextBytes = 320
	// MaximumSemanticScopeBytes bounds a NamingScope's semantic-scope label.
	MaximumSemanticScopeBytes = 256
	// MaximumNamingExceptions bounds how many reviewed naming-exception
	// directives one canonical name may carry.
	MaximumNamingExceptions = 8
)

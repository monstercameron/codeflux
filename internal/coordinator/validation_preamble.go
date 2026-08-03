package coordinator

// validationPreamble opens an instruction with what is actually known about
// the suite, rather than asserting it.
//
// Three instruction builders opened with the fixed words "The code compiles
// and its tests pass, but…". That is a claim, and it was stated whether or not
// anything had established it. On ladder rung 10 the produced program looped
// forever, its test run was cancelled, and the model was then told its tests
// passed and asked to go and improve its coverage — working from a premise the
// run had every reason to know was false.
//
// Saying less is the point. "The code compiles" is established by the assembly
// stage before any of these instructions are built; whether the suite passed
// is not, unless it did.
func validationPreamble(testsPassed bool) string {
	if testsPassed {
		return "The code compiles and its tests pass, but "
	}
	return "The code compiles, but its tests have not been shown to pass, and "
}

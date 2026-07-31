package shortcuts

// IsBrowserReserved reports whether a chord is owned by common browser chrome
// on the requested platform. Policies reject these chords rather than relying
// on preventDefault to override navigation, tab, window, or developer controls.
func IsBrowserReserved(chord Chord, platform Platform) bool {
	signature := chordSignature(chord)
	if _, found := reservedAllPlatforms[signature]; found {
		return true
	}
	if platform == PlatformMacOS {
		_, found := reservedMacOS[signature]
		return found
	}
	_, found := reservedControlPlatforms[signature]
	return found
}

var reservedAllPlatforms = map[string]struct{}{
	chordSignature(Chord{Key: "tab", Primary: true}):                 {},
	chordSignature(Chord{Key: "tab", Primary: true, Shift: true}):    {},
	chordSignature(Chord{Key: "arrowleft", Alt: true}):               {},
	chordSignature(Chord{Key: "arrowright", Alt: true}):              {},
	chordSignature(Chord{Key: "f4", Alt: true}):                      {},
	chordSignature(Chord{Key: "w", Primary: true}):                   {},
	chordSignature(Chord{Key: "t", Primary: true}):                   {},
	chordSignature(Chord{Key: "n", Primary: true}):                   {},
	chordSignature(Chord{Key: "r", Primary: true}):                   {},
	chordSignature(Chord{Key: "l", Primary: true}):                   {},
	chordSignature(Chord{Key: "p", Primary: true}):                   {},
	chordSignature(Chord{Key: "f", Primary: true}):                   {},
	chordSignature(Chord{Key: "s", Primary: true}):                   {},
	chordSignature(Chord{Key: "d", Primary: true}):                   {},
	chordSignature(Chord{Key: "h", Primary: true}):                   {},
	chordSignature(Chord{Key: "j", Primary: true}):                   {},
	chordSignature(Chord{Key: "o", Primary: true}):                   {},
	chordSignature(Chord{Key: "u", Primary: true}):                   {},
	chordSignature(Chord{Key: "0", Primary: true}):                   {},
	chordSignature(Chord{Key: "+", Primary: true}):                   {},
	chordSignature(Chord{Key: "-", Primary: true}):                   {},
	chordSignature(Chord{Key: "t", Primary: true, Shift: true}):      {},
	chordSignature(Chord{Key: "w", Primary: true, Shift: true}):      {},
	chordSignature(Chord{Key: "a", Primary: true, Shift: true}):      {},
	chordSignature(Chord{Key: "b", Primary: true, Shift: true}):      {},
	chordSignature(Chord{Key: "o", Primary: true, Shift: true}):      {},
	chordSignature(Chord{Key: "p", Primary: true, Shift: true}):      {},
	chordSignature(Chord{Key: "delete", Primary: true, Shift: true}): {},
	chordSignature(Chord{Key: "g", Primary: true, Alt: true}):        {},
	chordSignature(Chord{Key: "p", Primary: true, Alt: true}):        {},
}

var reservedMacOS = map[string]struct{}{
	chordSignature(Chord{Key: "c", Primary: true, Alt: true}):          {},
	chordSignature(Chord{Key: "i", Primary: true, Alt: true}):          {},
	chordSignature(Chord{Key: "j", Primary: true, Alt: true}):          {},
	chordSignature(Chord{Key: "k", Primary: true, Alt: true}):          {},
	chordSignature(Chord{Key: "e", Primary: true, Alt: true}):          {},
	chordSignature(Chord{Key: "m", Primary: true, Alt: true}):          {},
	chordSignature(Chord{Key: "u", Primary: true, Alt: true}):          {},
	chordSignature(Chord{Key: "h", Primary: true, Alt: true}):          {},
	chordSignature(Chord{Key: "arrowleft", Primary: true, Alt: true}):  {},
	chordSignature(Chord{Key: "arrowright", Primary: true, Alt: true}): {},
	chordSignature(Chord{Key: "arrowup", Primary: true, Alt: true}):    {},
	chordSignature(Chord{Key: "arrowdown", Primary: true, Alt: true}):  {},
}

var reservedControlPlatforms = map[string]struct{}{
	chordSignature(Chord{Key: "c", Primary: true, Shift: true}): {},
	chordSignature(Chord{Key: "i", Primary: true, Shift: true}): {},
	chordSignature(Chord{Key: "j", Primary: true, Shift: true}): {},
	chordSignature(Chord{Key: "n", Primary: true, Shift: true}): {},
	chordSignature(Chord{Key: "z", Primary: true, Alt: true}):   {},
}

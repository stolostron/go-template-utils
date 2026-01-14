package lint

// bytePosToColumn converts a byte position in a string to a 1-based column number.
// It counts runes (not bytes) to properly handle UTF-8 multi-byte characters like emoji.
// Tabs are expanded to the next multiple of 4 columns.
func bytePosToColumn(s string, bytePos int) int {
	if bytePos < 0 || bytePos > len(s) {
		return 0
	}

	column := 1

	// Go's range automatically iterates through strings rune by rune
	// i is the byte position of the start of the rune, r is the rune value
	for i, r := range s {
		// If we've reached or passed the target byte position, stop
		if i >= bytePos {
			break
		}

		// Update column based on the rune
		if r == '\t' {
			// Tab expands to next multiple of 4
			column = ((column-1)/4 + 1) * 4
		} else {
			column++
		}
	}

	return column
}

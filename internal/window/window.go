// Package window splits long state text into overlapping, token-bounded
// pieces. The splitter works on runes so a window never cuts a multi-byte
// character in half, and it uses the same estimate as the length guard so the
// limits shown in the TUI match the limits used here.
package window

import "playa/internal/lengthguard"

const (
	// DefaultSize is the initial window size in estimated tokens.
	DefaultSize = 32
	// DefaultOverlap is the initial overlap in estimated tokens.
	DefaultOverlap = 8
)

const unitsPerToken = 4

// Window is one piece of the source text.
type Window struct {
	Index  int
	Start  int // rune offset, inclusive
	End    int // rune offset, exclusive
	Text   string
	Tokens int // estimated tokens
}

// Split divides text into windows of at most size estimated tokens. Adjacent
// windows overlap by about overlap tokens. Empty text returns no windows.
//
// An overlap that would not leave room for progress is clamped to size-1, and
// a window always contains at least one rune.
func Split(text string, size, overlap int) []Window {
	if text == "" || size <= 0 {
		return nil
	}
	if overlap < 0 {
		overlap = 0
	}
	if overlap >= size {
		overlap = size - 1
	}

	runes := []rune(text)
	budget := size * unitsPerToken
	overlapUnits := overlap * unitsPerToken

	var out []Window
	for start := 0; start < len(runes); {
		end, units := start, 0
		for end < len(runes) {
			u := runeUnits(runes[end])
			// Always take the first rune so a pathological size cannot stall.
			if end > start && units+u > budget {
				break
			}
			units += u
			end++
		}

		piece := string(runes[start:end])
		out = append(out, Window{
			Index:  len(out),
			Start:  start,
			End:    end,
			Text:   piece,
			Tokens: lengthguard.EstimateTokens(piece),
		})
		if end >= len(runes) {
			break
		}

		next := end
		if overlapUnits > 0 {
			u := 0
			for next > start+1 && u < overlapUnits {
				next--
				u += runeUnits(runes[next])
			}
		}
		if next <= start {
			next = start + 1
		}
		start = next
	}
	return out
}

// runeUnits returns a rune's cost in quarter-tokens. This mirrors
// lengthguard.EstimateTokens: four narrow characters or one wide character
// make one token.
func runeUnits(r rune) int {
	if r >= 0x2E80 {
		return unitsPerToken
	}
	return 1
}

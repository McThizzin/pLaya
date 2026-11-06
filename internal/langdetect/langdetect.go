// Package langdetect runs `laya detect-lang` and parses its plain-text output.
//
// The command loads no model, so it is cheap enough to run while the user types.
// It reports the script and the english-vs-multilingual route the server would
// take for the same text.
package langdetect

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Preview is the parsed output of `laya detect-lang`.
type Preview struct {
	Script      string  // latin, cyrillic, cjk, ...
	English     bool    // the english: yes/no line
	Words       int     // word count from the words line
	EnglishHits int     // English function-word hits
	Ratio       float64 // English hits / words
	Route       string  // english, multilingual, ...
	Reason      string  // comparable to the server's route string
}

// Family returns the model family implied by the route, if it is one we know.
func (p Preview) Family() string {
	switch p.Route {
	case "english", "multilingual", "typed-decisions", "kev-0.8b":
		return p.Route
	}
	return ""
}

// Parse reads the key: value lines printed by `laya detect-lang`.
func Parse(out string) (Preview, error) {
	var p Preview
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "script:"):
			p.Script = value(line)
		case strings.HasPrefix(line, "english:"):
			v := strings.ToLower(value(line))
			p.English = v == "yes" || v == "true"
		case strings.HasPrefix(line, "words:"):
			p.Words, p.EnglishHits, p.Ratio = parseWords(line)
		case strings.HasPrefix(line, "route:"):
			p.Route = value(line)
		case strings.HasPrefix(line, "reason:"):
			p.Reason = value(line)
		}
	}
	if p.Route == "" {
		return Preview{}, errors.New("detect-lang output has no route line")
	}
	return p, nil
}

func value(line string) string {
	if i := strings.IndexByte(line, ':'); i >= 0 {
		return strings.TrimSpace(line[i+1:])
	}
	return ""
}

// parseWords reads a line like:
//
//	words: 6  english_hits: 4  ratio: 0.666667
func parseWords(line string) (words, hits int, ratio float64) {
	fields := strings.Fields(line)
	for i := 0; i+1 < len(fields); i += 2 {
		label, val := strings.TrimSuffix(fields[i], ":"), fields[i+1]
		switch label {
		case "words":
			words, _ = strconv.Atoi(val)
		case "english_hits":
			hits, _ = strconv.Atoi(val)
		case "ratio":
			ratio, _ = strconv.ParseFloat(val, 64)
		}
	}
	return words, hits, ratio
}

// Detect runs `laya detect-lang --text <text>` and parses the result. An empty
// path means "laya" from PATH.
func Detect(ctx context.Context, path, text string) (Preview, error) {
	if path == "" {
		path = "laya"
	}
	out, err := exec.CommandContext(ctx, path, "detect-lang", "--text", text).CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return Preview{}, ctx.Err()
		}
		if msg := strings.TrimSpace(string(out)); msg != "" {
			if line, _, _ := strings.Cut(msg, "\n"); line != "" {
				return Preview{}, fmt.Errorf("%s: %s", path, line)
			}
		}
		return Preview{}, fmt.Errorf("run %s detect-lang: %w", path, err)
	}
	return Parse(string(out))
}

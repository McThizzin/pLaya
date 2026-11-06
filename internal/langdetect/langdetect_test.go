package langdetect

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const englishOut = `script: latin
english: yes
words: 6  english_hits: 4  ratio: 0.666667
route: english
reason: english words 4/6
`

const multilingualOut = `script: latin
english: no
words: 5  english_hits: 0  ratio: 0
route: multilingual
reason: latin but weak english 0/5
`

func TestParse(t *testing.T) {
	p, err := Parse(englishOut)
	if err != nil {
		t.Fatal(err)
	}
	if p.Script != "latin" || !p.English || p.Words != 6 || p.EnglishHits != 4 {
		t.Errorf("preview = %+v", p)
	}
	if p.Ratio < 0.66 || p.Ratio > 0.67 {
		t.Errorf("ratio = %v", p.Ratio)
	}
	if p.Route != "english" || p.Reason != "english words 4/6" {
		t.Errorf("route/reason = %q / %q", p.Route, p.Reason)
	}
	if got := p.Family(); got != "english" {
		t.Errorf("Family() = %q", got)
	}

	p, err = Parse(multilingualOut)
	if err != nil {
		t.Fatal(err)
	}
	if p.English || p.Route != "multilingual" || p.Family() != "multilingual" {
		t.Errorf("multilingual preview = %+v", p)
	}
}

func TestParseRejectsMissingRoute(t *testing.T) {
	if _, err := Parse("script: latin\n"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestDetectRunsTheCLI(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-laya")
	body := "#!/bin/sh\n[ \"$1\" = detect-lang ] && [ \"$2\" = --text ] || exit 2\necho 'script: latin'\necho 'english: yes'\necho 'words: 2  english_hits: 2  ratio: 1'\necho 'route: english'\necho \"reason: $3\"\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	p, err := Detect(context.Background(), script, "hello world")
	if err != nil {
		t.Fatal(err)
	}
	if p.Route != "english" || p.Reason != "hello world" {
		t.Errorf("preview = %+v", p)
	}
}

func TestDetectReportsCLIError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing")
	_, err := Detect(context.Background(), path, "x")
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("err = %v", err)
	}
}

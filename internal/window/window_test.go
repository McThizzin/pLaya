package window

import (
	"strings"
	"testing"
)

func TestSplitEmptyAndShort(t *testing.T) {
	if got := Split("", 32, 8); got != nil {
		t.Errorf("empty text = %v, want nil", got)
	}
	if got := Split("small", 0, 8); got != nil {
		t.Errorf("zero size = %v, want nil", got)
	}
	got := Split("small", 32, 8)
	if len(got) != 1 {
		t.Fatalf("windows = %d, want 1", len(got))
	}
	if got[0].Text != "small" || got[0].Start != 0 || got[0].End != 5 {
		t.Errorf("window = %+v", got[0])
	}
}

func TestSplitCoversTheText(t *testing.T) {
	text := strings.Repeat("abcdefghij", 200)
	windows := Split(text, 32, 8)
	if len(windows) < 2 {
		t.Fatalf("windows = %d, want several", len(windows))
	}
	if windows[len(windows)-1].End != len([]rune(text)) {
		t.Errorf("last end = %d, want %d", windows[len(windows)-1].End, len([]rune(text)))
	}
	covered := make([]bool, len([]rune(text)))
	for _, w := range windows {
		if w.Tokens > 32 {
			t.Errorf("window %d has %d tokens, want <= 32", w.Index, w.Tokens)
		}
		if w.Text == "" {
			t.Errorf("window %d is empty", w.Index)
		}
		for i := w.Start; i < w.End; i++ {
			covered[i] = true
		}
	}
	for i, ok := range covered {
		if !ok {
			t.Fatalf("rune %d is not covered", i)
		}
	}
}

func TestSplitOverlapsAndProgresses(t *testing.T) {
	text := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 20)
	windows := Split(text, 32, 8)
	if len(windows) < 2 {
		t.Fatalf("windows = %d, want several", len(windows))
	}
	for i := 1; i < len(windows); i++ {
		prev, cur := windows[i-1], windows[i]
		if cur.Start <= prev.Start {
			t.Errorf("window %d starts at %d, previous at %d", i, cur.Start, prev.Start)
		}
		if cur.End <= prev.End {
			t.Errorf("window %d ends at %d, previous at %d", i, cur.End, prev.End)
		}
		if cur.Start >= prev.End {
			t.Errorf("window %d does not overlap: start %d, previous end %d", i, cur.Start, prev.End)
		}
	}
}

func TestSplitWideCharacters(t *testing.T) {
	text := strings.Repeat("漢", 100)
	windows := Split(text, 32, 0)
	if len(windows) != 4 {
		t.Fatalf("windows = %d, want 4", len(windows))
	}
	for _, w := range windows {
		if w.Tokens > 32 {
			t.Errorf("window %d has %d tokens, want <= 32", w.Index, w.Tokens)
		}
	}
	if got := windows[0].Tokens; got != 32 {
		t.Errorf("first window tokens = %d, want 32", got)
	}
	if got := windows[3].Text; got != strings.Repeat("漢", 4) {
		t.Errorf("last window = %q", got)
	}
}

func TestSplitClampsOverlap(t *testing.T) {
	text := strings.Repeat("x", 500)
	windows := Split(text, 16, 100)
	if len(windows) < 2 {
		t.Fatalf("windows = %d, want several", len(windows))
	}
	if windows[len(windows)-1].End != len([]rune(text)) {
		t.Errorf("last end = %d, want %d", windows[len(windows)-1].End, len([]rune(text)))
	}
	for i := 1; i < len(windows); i++ {
		if windows[i].Start <= windows[i-1].Start {
			t.Fatalf("window %d did not progress", i)
		}
	}
}

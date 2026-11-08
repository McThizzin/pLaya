package snippet

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// configHome points os.UserConfigDir at a temporary directory.
func configHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return dir
}

func TestSaveLoadAndAll(t *testing.T) {
	home := configHome(t)
	want := Snippet{
		Preset:   "guard",
		StateKey: "prompt",
		State: map[string]any{
			"prompt": "A long document about a refund",
			"count":  float64(7),
			"nested": map[string]any{"ok": true},
		},
	}
	path, err := Save("Support / Refund", want)
	if err != nil {
		t.Fatal(err)
	}
	if wantPath := filepath.Join(home, "playa", "snippets", "support_refund.json"); path != wantPath {
		t.Errorf("path = %q, want %q", path, wantPath)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 600", info.Mode().Perm())
	}

	got, err := Load("Support / Refund")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("loaded snippet = %#v, want %#v", got, want)
	}

	if _, err := Save("second", Snippet{StateKey: "text", State: map[string]any{"text": "two"}}); err != nil {
		t.Fatal(err)
	}
	names, err := All()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"second", "support_refund"}; !reflect.DeepEqual(names, want) {
		t.Errorf("All = %v, want %v", names, want)
	}
}

func TestAllMissingDirectory(t *testing.T) {
	configHome(t)
	names, err := All()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 0 {
		t.Errorf("All = %v, want none", names)
	}
}

func TestDelete(t *testing.T) {
	configHome(t)
	if _, err := Save("temp", Snippet{StateKey: "text", State: map[string]any{"text": "x"}}); err != nil {
		t.Fatal(err)
	}
	if err := Delete("temp"); err != nil {
		t.Fatal(err)
	}
	if _, err := Load("temp"); !os.IsNotExist(err) {
		t.Errorf("Load after Delete = %v, want not exist", err)
	}
	// Deleting a missing snippet is not an error: the list may be stale.
	if err := Delete("temp"); err != nil {
		t.Errorf("second Delete = %v", err)
	}
}

func TestSlugPreventsTraversal(t *testing.T) {
	configHome(t)
	path, err := Path("../../etc/passwd")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "etcpasswd.json" {
		t.Errorf("unsafe file name %q", filepath.Base(path))
	}
	if strings.Contains(filepath.Clean(path), "..") {
		t.Errorf("path escapes the snippet dir: %q", path)
	}
}

func TestSaveRejectsIncompleteSnippet(t *testing.T) {
	configHome(t)
	for _, c := range []struct {
		name string
		s    Snippet
		want string
	}{
		{"no key", Snippet{State: map[string]any{"x": "y"}}, "state key"},
		{"no state", Snippet{StateKey: "text"}, "state"},
		{"!!!", Snippet{StateKey: "text", State: map[string]any{"text": "x"}}, "name"},
	} {
		_, err := Save(c.name, c.s)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
	}
}

func TestLoadRejectsBadFiles(t *testing.T) {
	home := configHome(t)
	dir := filepath.Join(home, "playa", "snippets")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, body, want string
	}{
		{"unknown", `{"state_key":"text","state":{"text":"x"},"extra":1}`, "unknown field"},
		{"no-key", `{"state":{"text":"x"}}`, "no state key"},
		{"no-state", `{"state_key":"text"}`, "no state"},
	} {
		if err := os.WriteFile(filepath.Join(dir, Slug(c.name)+".json"), []byte(c.body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(c.name); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
	}
}

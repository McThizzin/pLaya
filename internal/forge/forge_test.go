package forge

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"playa/internal/api"
)

func TestParseChoice(t *testing.T) {
	opts, err := ParseChoice(`
# tone options
calm: no strong feeling
angry
billing: charged twice: urgent
`)
	if err != nil {
		t.Fatal(err)
	}
	want := []api.Option{
		{Key: "calm", Description: "no strong feeling"},
		{Key: "angry"},
		{Key: "billing", Description: "charged twice: urgent"},
	}
	if !reflect.DeepEqual(opts, want) {
		t.Errorf("options = %+v", opts)
	}
}

func TestParseChoiceErrors(t *testing.T) {
	for _, c := range []struct {
		name, text, want string
	}{
		{"empty", "\n# nothing\n", "at least one option"},
		{"duplicate", "a\na", "duplicate"},
		{"too many", strings.Join(strings.Fields("a b c d e f g h i j k l m n o p q"), "\n"), "max is 16"},
		{"empty key", ": description", "empty option key"},
	} {
		_, err := ParseChoice(c.text)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
	}
}

func TestChoiceCriteriaKeepOrder(t *testing.T) {
	opts := []api.Option{{Key: "zeta"}, {Key: "alpha", Description: "first"}, {Key: "mike"}}
	q, err := Question(api.TypeChoice, "Which one?", opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Go maps sort keys; the raw criteria must not.
	if got := string(q.Criteria); got != `{"zeta":null,"alpha":"first","mike":null}` {
		t.Errorf("criteria = %s", got)
	}
	got := q.Options()
	if got[0].Key != "zeta" || got[1].Key != "alpha" || got[2].Key != "mike" {
		t.Errorf("round-trip order = %+v", got)
	}
}

func TestScoreAndNoulCriteria(t *testing.T) {
	q, err := Question(api.TypeScore, "How urgent?", nil, []string{"low", "high"})
	if err != nil {
		t.Fatal(err)
	}
	if string(q.Criteria) != `["low","high"]` {
		t.Errorf("score criteria = %s", q.Criteria)
	}

	noul, err := Question(api.TypeNoul, "Is it spam?", []api.Option{{Key: "false"}, {Key: "true", Description: "yes it is"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(noul.Criteria) != `{"false":null,"true":"yes it is"}` {
		t.Errorf("noul criteria = %s", noul.Criteria)
	}

	plain, err := Question(api.TypeNoul, "Is it spam?", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if plain.Criteria != nil {
		t.Errorf("noul without descriptions should have nil criteria, got %s", plain.Criteria)
	}
}

func TestValidate(t *testing.T) {
	if err := Validate(api.TypeChoice, "", []api.Option{{Key: "a"}}, nil); err == nil {
		t.Error("empty instructions should fail")
	}
	if err := Validate(api.TypeScore, "?", nil, []string{"only one"}); !errors.Is(err, ErrNoLevels) {
		t.Errorf("one level: %v", err)
	}
	if err := Validate(api.TypeChoice, "?", []api.Option{{Key: "a"}}, nil); err != nil {
		t.Errorf("valid choice: %v", err)
	}
}

func TestCloneAndNextName(t *testing.T) {
	orig := api.QuestionSet{{Name: "a", Question: api.Question{Type: api.TypeChoice, Criteria: json.RawMessage(`{"x":null}`)}}}
	clone := Clone(orig)
	clone[0].Question.Criteria = json.RawMessage(`{"y":null}`)
	if string(orig[0].Question.Criteria) != `{"x":null}` {
		t.Error("Clone must not share the criteria slice")
	}
	if got := NextName(orig, "A"); got != "a_2" {
		t.Errorf("NextName = %q", got)
	}
	if got := Slug("  My New Question! "); got != "my_new_question" {
		t.Errorf("Slug = %q", got)
	}
}

func TestSaveLoadList(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	set := Set{
		Preset: "email",
		Questions: api.QuestionSet{
			{Name: "category", Question: api.Question{Type: api.TypeChoice, Instructions: "Which?", Criteria: json.RawMessage(`{"billing":null,"sales":null}`)}},
			{Name: "urgent", Question: api.Question{Type: api.TypeNoul, Instructions: "Urgent?"}},
		},
	}
	path, err := Save("My Set", set)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "my_set.json" {
		t.Errorf("path = %s", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 600", info.Mode().Perm())
	}

	names, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names, []string{"my_set"}) {
		t.Errorf("list = %v", names)
	}

	got, err := Load("my_set")
	if err != nil {
		t.Fatal(err)
	}
	if got.Preset != "email" || got.Questions.Names()[0] != "category" {
		t.Errorf("loaded = %+v", got)
	}
	var compacted bytes.Buffer
	if err := json.Compact(&compacted, got.Questions[0].Criteria); err != nil {
		t.Fatal(err)
	}
	if compacted.String() != `{"billing":null,"sales":null}` {
		t.Errorf("criteria order lost: %s", got.Questions[0].Criteria)
	}
}

func TestLoadMissingAndEmptyList(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	names, err := List()
	if err != nil || len(names) != 0 {
		t.Errorf("empty list = %v, %v", names, err)
	}
	if _, err := Load("nope"); err == nil {
		t.Error("missing set should fail")
	}
}

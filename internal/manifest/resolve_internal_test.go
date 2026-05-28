package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormatRefVersion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		version any
		want    string
		ok      bool
	}{
		{name: "nil", version: nil, ok: false},
		{name: "empty string", version: "  ", ok: false},
		{name: "string", version: "beta", want: "beta", ok: true},
		{name: "int", version: 2, want: "2", ok: true},
		{name: "int64", version: int64(3), want: "3", ok: true},
		{name: "uint64", version: uint64(4), want: "4", ok: true},
		{name: "float whole", version: float64(5), want: "5", ok: true},
		{name: "float fractional", version: 1.5, want: "1.5", ok: true},
		{name: "default type", version: true, want: "true", ok: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := formatRefVersion(tc.version)
			if ok != tc.ok {
				t.Fatalf("formatRefVersion() ok = %v, want %v", ok, tc.ok)
			}
			if got != tc.want {
				t.Fatalf("formatRefVersion() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBundleRefCandidates(t *testing.T) {
	t.Parallel()
	noVer := bundleRefCandidates("prompts/sys", nil, []string{".yaml", ".md"})
	if len(noVer) != 2 || noVer[0] != "prompts/sys.yaml" || noVer[1] != "prompts/sys.md" {
		t.Fatalf("without version = %v", noVer)
	}
	withVer := bundleRefCandidates("prompts/sys", 2, []string{".yaml"})
	want := []string{
		"prompts/sys-2.yaml",
		filepath.Join("prompts/sys", "2.yaml"),
		filepath.Join("prompts/sys", "v2.yaml"),
	}
	if len(withVer) != len(want) {
		t.Fatalf("with version = %v, want %v", withVer, want)
	}
	for i := range want {
		if withVer[i] != want[i] {
			t.Fatalf("candidate[%d] = %q, want %q", i, withVer[i], want[i])
		}
	}
}

func TestIsPathWithinRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	inside := filepath.Join(root, "nested", "file.yaml")
	if err := os.MkdirAll(filepath.Dir(inside), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if !isPathWithinRoot(root, inside) {
		t.Fatalf("expected %q inside %q", inside, root)
	}
	outside := filepath.Join(filepath.Dir(root), "outside.yaml")
	if isPathWithinRoot(root, outside) {
		t.Fatalf("expected %q outside %q", outside, root)
	}
	if isPathWithinRoot("", inside) {
		t.Fatal("empty root: want false")
	}
	if isPathWithinRoot(root, "") {
		t.Fatal("empty path: want false")
	}
}

func TestLocateBundleFile_rejectsPathEscape(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	_, err := locateBundleFile(root, "../../outside", nil, refKindInstructions)
	if err == nil {
		t.Fatal("path escape: want error")
	}
	if !strings.Contains(err.Error(), "no candidates") {
		t.Fatalf("error = %v, want no candidates detail", err)
	}
}

func TestLocateBundleFile_errors(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	_, err := locateBundleFile(root, "", nil, refKindInstructions)
	if err == nil || !strings.Contains(err.Error(), "ref is empty") {
		t.Fatalf("empty ref error = %v", err)
	}

	_, err = locateBundleFile(root, "/abs/prompt.yaml", nil, refKindSchema)
	if err == nil {
		t.Fatal("absolute ref: want error")
	}
	var fieldErr FieldError
	if !errorsAsFieldError(err, &fieldErr) || fieldErr.Path != "output.schema.ref" {
		t.Fatalf("absolute ref error = %v", err)
	}

	_, err = locateBundleFile(root, "missing", "v1", refKindInstructions)
	if err == nil {
		t.Fatal("missing file: want error")
	}
	if !errorsAsFieldError(err, &fieldErr) || fieldErr.Path != "spec.instructions.ref" {
		t.Fatalf("missing file error = %v", err)
	}
	if !strings.Contains(err.Error(), `version "v1"`) {
		t.Fatalf("error = %v, want version in message", err)
	}
}

func TestCloneAgent_copiesNestedFields(t *testing.T) {
	t.Parallel()
	strict := true
	agent := &Agent{
		APIVersion: APIVersionV1,
		Kind:       KindAgent,
		Metadata: AgentMetadata{
			Name:      "a",
			Namespace: "ns",
			Version:   "1.0.0",
			Labels:    map[string]string{"k": "v"},
		},
		Spec: AgentSpec{
			Purpose:      "p",
			Instructions: InstructionsSpec{Text: "t"},
			Model: ModelConfig{
				Provider:        "anthropic",
				Name:            "claude",
				Parameters:      &ModelParameters{Temperature: ptrFloat(0.1)},
				Reasoning:       &ReasoningConfig{Effort: "low"},
				ProviderOptions: map[string]any{"k": 1},
			},
			Limits: &Limits{OnLimit: "halt"},
		},
		Output: &OutputSpec{
			Format: "json",
			Schema: &SchemaSpec{Inline: map[string]any{"type": "object"}},
			Strict: &strict,
		},
	}
	cloned := cloneAgent(agent)
	if cloned.Metadata.Labels["k"] != "v" {
		t.Fatal("labels not copied")
	}
	if cloned.Spec.Model.ProviderOptions["k"] != 1 {
		t.Fatal("provider_options not copied")
	}
	if cloned.Output.Schema.Inline["type"] != "object" {
		t.Fatal("schema inline not copied")
	}
	cloned.Metadata.Labels["k"] = "changed"
	if agent.Metadata.Labels["k"] == "changed" {
		t.Fatal("labels not deep-copied")
	}
}

func ptrFloat(f float64) *float64 {
	return &f
}

func TestLoadInstructionsFile_readError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	dirPath := filepath.Join(dir, "promptdir")
	if err := os.Mkdir(dirPath, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if _, err := loadInstructionsFile(dirPath); err == nil {
		t.Fatal("directory path: want read error")
	}
}

func TestLoadInstructionsFile_formats(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	mdPath := filepath.Join(dir, "p.md")
	if err := os.WriteFile(mdPath, []byte("  md body  "), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	text, err := loadInstructionsFile(mdPath)
	if err != nil || text != "md body" {
		t.Fatalf("md: text=%q err=%v", text, err)
	}

	contentPath := filepath.Join(dir, "p.yaml")
	if err := os.WriteFile(contentPath, []byte("content: |\n  from content\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	text, err = loadInstructionsFile(contentPath)
	if err != nil || text != "from content" {
		t.Fatalf("content field: text=%q err=%v", text, err)
	}

	textPath := filepath.Join(dir, "p-text.yaml")
	if err := os.WriteFile(textPath, []byte("text: inline prompt\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	text, err = loadInstructionsFile(textPath)
	if err != nil || text != "inline prompt" {
		t.Fatalf("text field: text=%q err=%v", text, err)
	}

	fallbackPath := filepath.Join(dir, "p-fallback.yaml")
	if err := os.WriteFile(fallbackPath, []byte("foo: bar\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	text, err = loadInstructionsFile(fallbackPath)
	if err != nil || text != "foo: bar" {
		t.Fatalf("yaml fallback raw: text=%q err=%v", text, err)
	}

	badPath := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(badPath, []byte("text: [\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err = loadInstructionsFile(badPath)
	if err == nil || !strings.Contains(err.Error(), "parse prompt YAML") {
		t.Fatalf("bad yaml: err=%v", err)
	}

	ymlPath := filepath.Join(dir, "p.yml")
	if err := os.WriteFile(ymlPath, []byte("text: from yml extension\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	text, err = loadInstructionsFile(ymlPath)
	if err != nil || text != "from yml extension" {
		t.Fatalf("yml: text=%q err=%v", text, err)
	}

	otherPath := filepath.Join(dir, "p.txt")
	if err := os.WriteFile(otherPath, []byte("  raw\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	text, err = loadInstructionsFile(otherPath)
	if err != nil || text != "raw" {
		t.Fatalf("default ext: text=%q err=%v", text, err)
	}
}

func TestLoadSchemaFile_errors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	missing := filepath.Join(dir, "nope.json")
	if _, err := loadSchemaFile(missing); err == nil {
		t.Fatal("missing file: want error")
	}

	badJSON := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(badJSON, []byte("{"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := loadSchemaFile(badJSON); err == nil || !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("bad json: err=%v", err)
	}

	empty := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(empty, []byte("{}"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := loadSchemaFile(empty); err == nil || !strings.Contains(err.Error(), "non-empty") {
		t.Fatalf("empty object: err=%v", err)
	}
}

func TestResolvedAgent_JSON_nil(t *testing.T) {
	t.Parallel()
	var nilResolved *ResolvedAgent
	if _, err := nilResolved.JSON(); err == nil {
		t.Fatal("nil receiver: want error")
	}
	if _, err := (&ResolvedAgent{}).JSON(); err == nil {
		t.Fatal("nil Agent: want error")
	}
}

func TestResolveBundle_nilAgent(t *testing.T) {
	t.Parallel()
	if _, err := ResolveBundle("agent.yaml", nil); err == nil {
		t.Fatal("ResolveBundle(nil): want error")
	}
}

func errorsAsFieldError(err error, target *FieldError) bool {
	fe, ok := err.(FieldError)
	if !ok {
		return false
	}
	*target = fe
	return true
}

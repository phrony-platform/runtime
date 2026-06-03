package envfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseFile_basic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := `# comment
export ANTHROPIC_API_KEY=sk-from-file
PHRONY_RUNTIME_ADDR=127.0.0.1:7777
QUOTED="hello world"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if got["ANTHROPIC_API_KEY"] != "sk-from-file" {
		t.Fatalf("ANTHROPIC_API_KEY = %q", got["ANTHROPIC_API_KEY"])
	}
	if got["PHRONY_RUNTIME_ADDR"] != "127.0.0.1:7777" {
		t.Fatalf("PHRONY_RUNTIME_ADDR = %q", got["PHRONY_RUNTIME_ADDR"])
	}
	if got["QUOTED"] != "hello world" {
		t.Fatalf("QUOTED = %q", got["QUOTED"])
	}
}

func TestApplyFile_setsUnsetVars(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("ANTHROPIC_API_KEY=sk-file\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("ANTHROPIC_API_KEY", "")

	if err := ApplyFile(path); err != nil {
		t.Fatalf("ApplyFile: %v", err)
	}
	if os.Getenv("ANTHROPIC_API_KEY") != "sk-file" {
		t.Fatalf("ANTHROPIC_API_KEY = %q, want sk-file", os.Getenv("ANTHROPIC_API_KEY"))
	}
}

func TestApplyFile_doesNotOverrideExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("ANTHROPIC_API_KEY=sk-file\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("ANTHROPIC_API_KEY", "sk-shell")

	if err := ApplyFile(path); err != nil {
		t.Fatalf("ApplyFile: %v", err)
	}
	if os.Getenv("ANTHROPIC_API_KEY") != "sk-shell" {
		t.Fatalf("ANTHROPIC_API_KEY = %q, want sk-shell", os.Getenv("ANTHROPIC_API_KEY"))
	}
}

func TestApplyFiles_missingFile(t *testing.T) {
	err := ApplyFiles([]string{filepath.Join(t.TempDir(), "missing.env")})
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

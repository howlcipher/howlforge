package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writeFile creates a file with content under dir, creating parents.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestNewLoaderRejectsNonDirectories(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "file.txt", "x")

	if _, err := NewLoader(filepath.Join(dir, "file.txt")); err == nil {
		t.Fatal("NewLoader accepted a regular file as a root")
	}
	if _, err := NewLoader(filepath.Join(dir, "missing")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("NewLoader on a missing root returned %v, want ErrNotFound", err)
	}
	if _, err := NewLoader("   "); err == nil {
		t.Fatal("NewLoader accepted an empty root")
	}
}

func TestReadFileRefusesEscapes(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeFile(t, outside, "secret.yaml", "secret: true")
	writeFile(t, root, "ok.yaml", "ok: true")

	loader, err := NewLoader(root)
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}

	escapes := []string{
		"../secret.yaml",
		"nested/../../secret.yaml",
		"..",
		filepath.Join(outside, "secret.yaml"),
	}
	for _, path := range escapes {
		t.Run(path, func(t *testing.T) {
			if _, err := loader.ReadFile(path); err == nil {
				t.Fatalf("ReadFile(%q) succeeded, want refusal", path)
			}
		})
	}

	if _, err := loader.ReadFile("ok.yaml"); err != nil {
		t.Fatalf("ReadFile on a legitimate file returned %v", err)
	}
}

func TestReadFileRefusesSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation is not reliable on windows runners")
	}
	root := t.TempDir()
	outside := t.TempDir()
	target := writeFile(t, outside, "secret.yaml", "secret: true")

	link := filepath.Join(root, "escape.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	loader, err := NewLoader(root)
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}
	// The file exists and is readable by the process, so only the confinement
	// check stands between a hostile config directory and an arbitrary read.
	if _, err := loader.ReadFile("escape.yaml"); err == nil {
		t.Fatal("ReadFile followed a symlink out of the configuration root")
	} else if !strings.Contains(err.Error(), "outside") {
		t.Fatalf("ReadFile returned %v, want a confinement refusal", err)
	}
}

func TestReadFileEnforcesSizeLimit(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "big.yaml", strings.Repeat("a", 200))

	loader, err := NewLoader(root, WithMaxFileBytes(64))
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}
	_, err = loader.ReadFile("big.yaml")
	if err == nil {
		t.Fatal("ReadFile accepted a file over the size limit")
	}
	if !strings.Contains(err.Error(), "limit") {
		t.Fatalf("ReadFile returned %v, want a size limit refusal", err)
	}
}

func TestReadFileRejectsDirectories(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	loader, err := NewLoader(root)
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}
	if _, err := loader.ReadFile("sub"); err == nil {
		t.Fatal("ReadFile accepted a directory")
	}
}

func TestListFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "items/b.yaml", "b: 1")
	writeFile(t, root, "items/a.yaml", "a: 1")
	writeFile(t, root, "items/c.yml", "c: 1")
	writeFile(t, root, "items/notes.txt", "ignored")
	writeFile(t, root, "items/.hidden.yaml", "ignored")
	writeFile(t, root, "items/deeper/d.yaml", "ignored")

	loader, err := NewLoader(root)
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}
	got, err := loader.ListFiles("items", ".yaml", ".yml")
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	want := []string{"items/a.yaml", "items/b.yaml", "items/c.yml"}
	if len(got) != len(want) {
		t.Fatalf("ListFiles = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ListFiles = %v, want %v (lexical order is required for determinism)", got, want)
		}
	}

	if _, err := loader.ListFiles("missing", ".yaml"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ListFiles on a missing directory returned %v, want ErrNotFound", err)
	}
}

func TestListFilesEnforcesCountLimit(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		writeFile(t, root, "items/"+name+".yaml", "x: 1")
	}
	loader, err := NewLoader(root, WithMaxFiles(2))
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}
	if _, err := loader.ListFiles("items", ".yaml"); err == nil {
		t.Fatal("ListFiles accepted a directory over the entry limit")
	}
}

type sample struct {
	Name  string `yaml:"name" json:"name"`
	Count int    `yaml:"count" json:"count"`
}

func TestDecodeYAMLStrict(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{"valid", "name: a\ncount: 2\n", ""},
		{"unknown field", "name: a\nextra: 1\n", "field extra not found"},
		{"empty file", "", "empty"},
		{"second document", "name: a\n---\nname: b\n", "more than one"},
		{"wrong type", "name: a\ncount: notanumber\n", "cannot unmarshal"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var target sample
			err := DecodeYAMLStrict([]byte(tt.input), &target)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("DecodeYAMLStrict returned %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("DecodeYAMLStrict accepted %q", tt.input)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("DecodeYAMLStrict returned %q, want it to mention %q", err, tt.wantErr)
			}
		})
	}
}

func TestDecodeJSONStrict(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{"valid", `{"name":"a","count":2}`, ""},
		{"unknown field", `{"name":"a","extra":1}`, "unknown field"},
		{"empty", ``, "empty"},
		{"trailing content", `{"name":"a"} {"name":"b"}`, "trailing content"},
		{"malformed", `{"name":`, "unexpected EOF"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var target sample
			err := DecodeJSONStrict([]byte(tt.input), &target)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("DecodeJSONStrict returned %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("DecodeJSONStrict accepted %q", tt.input)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("DecodeJSONStrict returned %q, want it to mention %q", err, tt.wantErr)
			}
		})
	}
}

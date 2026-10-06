package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeDotenv(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func TestLoadDotenvMissingFile(t *testing.T) {
	if err := LoadDotenv(filepath.Join(t.TempDir(), ".env")); err != nil {
		t.Fatalf("missing file: %v", err)
	}
}

func TestLoadDotenvSetsValues(t *testing.T) {
	path := writeDotenv(t, strings.Join([]string{
		"# a comment line",
		"",
		"DOTENV_PLAIN=one",
		"export DOTENV_EXPORT=two",
		`DOTENV_QUOTED="  spaced  "`,
		"DOTENV_TAIL=value # trailing comment",
		"DOTENV_EMPTY=",
	}, "\n"))
	for _, key := range []string{"DOTENV_PLAIN", "DOTENV_EXPORT", "DOTENV_QUOTED", "DOTENV_TAIL", "DOTENV_EMPTY"} {
		t.Cleanup(func() { _ = os.Unsetenv(key) })
	}

	if err := LoadDotenv(path); err != nil {
		t.Fatalf("load: %v", err)
	}
	checks := map[string]string{
		"DOTENV_PLAIN":  "one",
		"DOTENV_EXPORT": "two",
		"DOTENV_QUOTED": "  spaced  ",
		"DOTENV_TAIL":   "value",
		"DOTENV_EMPTY":  "",
	}
	for key, want := range checks {
		if got := os.Getenv(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if _, exists := os.LookupEnv("DOTENV_EMPTY"); !exists {
		t.Error("DOTENV_EMPTY was not set at all, want set to empty")
	}
}

func TestLoadDotenvShellWins(t *testing.T) {
	t.Setenv("DOTENV_SHELL", "from-shell")
	t.Setenv("DOTENV_SHELL_EMPTY", "")
	path := writeDotenv(t, "DOTENV_SHELL=from-file\nDOTENV_SHELL_EMPTY=from-file\n")
	t.Cleanup(func() { _ = os.Unsetenv("DOTENV_SHELL_EMPTY") })

	if err := LoadDotenv(path); err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := os.Getenv("DOTENV_SHELL"); got != "from-shell" {
		t.Errorf("DOTENV_SHELL = %q, want the shell value", got)
	}
	// An empty shell variable counts as set: the file must not fill it in.
	if got := os.Getenv("DOTENV_SHELL_EMPTY"); got != "" {
		t.Errorf("DOTENV_SHELL_EMPTY = %q, want empty", got)
	}
}

func TestLoadDotenvMalformed(t *testing.T) {
	path := writeDotenv(t, "DOTENV_BEFORE=kept\nthis line has no equals\n")
	t.Cleanup(func() { _ = os.Unsetenv("DOTENV_BEFORE") })

	err := LoadDotenv(path)
	if err == nil {
		t.Fatal("malformed line accepted")
	}
	if !strings.Contains(err.Error(), ":2:") || !strings.Contains(err.Error(), "KEY=VALUE") {
		t.Errorf("error %q lacks line number and hint", err)
	}
	// Parse-before-apply: nothing from a malformed file reaches the env.
	if _, exists := os.LookupEnv("DOTENV_BEFORE"); exists {
		t.Error("partial apply: DOTENV_BEFORE was set despite the error")
	}
}

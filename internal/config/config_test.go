package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := "# comment\r\n\r\nTDM_PLAIN=one\r\nexport TDM_EXPORTED=two\nTDM_DQ=\"three four\"\nTDM_SQ='five'\nTDM_EMPTY=\nTDM_SET=from-file\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TDM_SET", "from-env") // the real environment must win

	LoadDotEnv(path)

	for key, want := range map[string]string{
		"TDM_PLAIN":    "one",
		"TDM_EXPORTED": "two",
		"TDM_DQ":       "three four",
		"TDM_SQ":       "five",
		"TDM_SET":      "from-env",
	} {
		if got := os.Getenv(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
		if key != "TDM_SET" {
			os.Unsetenv(key)
		}
	}
	if _, set := os.LookupEnv("TDM_EMPTY"); set {
		t.Error("empty value should not be set")
	}
}

func TestLoadDotEnvMissingFile(t *testing.T) {
	LoadDotEnv(filepath.Join(t.TempDir(), "nope.env")) // must not panic
}

package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// LoadDotenv reads a dotenv file and sets every key not already present in
// the environment, so shell variables win over the file. A missing file is
// not an error; a malformed line is, and the caller stops the server rather
// than run with a typo'd value. The syntax is the format this project
// documents: KEY=VALUE lines, blank lines, # comments, an optional "export "
// prefix, a trailing " # comment" on unquoted values, and one matching pair
// of surrounding quotes.
func LoadDotenv(path string) error {
	data, err := os.ReadFile(path) // #nosec G304 -- path is the caller's config file, not user input
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	// Parse everything before touching the environment, so a malformed file
	// leaves it untouched (the caller aborts anyway).
	vals := map[string]string{}
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, found := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !found || key == "" {
			return fmt.Errorf("%s:%d: malformed line, want KEY=VALUE", path, i+1)
		}
		vals[key] = parseValue(value)
	}

	for key, value := range vals {
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("set %s: %w", key, err)
		}
	}
	return nil
}

// parseValue trims surrounding space, strips one matching pair of surrounding
// quotes, and on unquoted values drops a trailing " # comment" tail.
func parseValue(v string) string {
	v = strings.TrimSpace(v)
	if !quoted(v) {
		for i := 0; i < len(v); i++ {
			if v[i] == '#' && i > 0 && (v[i-1] == ' ' || v[i-1] == '\t') {
				v = strings.TrimSpace(v[:i])
				break
			}
		}
	}
	if quoted(v) {
		return v[1 : len(v)-1]
	}
	return v
}

// quoted reports whether v is wrapped in a matching pair of quotes.
func quoted(v string) bool {
	return len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0]
}

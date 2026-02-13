package env

import (
	"bufio"
	"errors"
	"os"
	"strconv"
	"strings"
)

// LoadDotEnv loads KEY=VALUE pairs from dotenv-style files into process env.
// Existing environment variables are preserved.
func LoadDotEnv(paths ...string) error {
	for _, p := range paths {
		if err := loadFile(p); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
	}
	return nil
}

func loadFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}

		val := parseValue(strings.TrimSpace(v))
		if err := os.Setenv(key, val); err != nil {
			return err
		}
	}
	return sc.Err()
}

func parseValue(v string) string {
	if len(v) >= 2 && strings.HasPrefix(v, "\"") && strings.HasSuffix(v, "\"") {
		if unquoted, err := strconv.Unquote(v); err == nil {
			return unquoted
		}
	}
	if len(v) >= 2 && strings.HasPrefix(v, "'") && strings.HasSuffix(v, "'") {
		return v[1 : len(v)-1]
	}
	return v
}

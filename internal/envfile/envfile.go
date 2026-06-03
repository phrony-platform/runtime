package envfile

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// ApplyFile reads KEY=VALUE entries from path and sets them in the process
// environment. Variables already set in the environment are left unchanged.
func ApplyFile(path string) error {
	vars, err := ParseFile(path)
	if err != nil {
		return err
	}
	for key, val := range vars {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			continue
		}
		if err := os.Setenv(key, val); err != nil {
			return fmt.Errorf("set %s: %w", key, err)
		}
	}
	return nil
}

// ApplyFiles loads env files in order; later files fill in keys still unset.
func ApplyFiles(paths []string) error {
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		if err := ApplyFile(path); err != nil {
			return fmt.Errorf("load env file %q: %w", path, err)
		}
	}
	return nil
}

// ParseFile reads a dotenv-style file into a map. Keys from later lines replace
// earlier ones in the returned map only; ApplyFile decides whether to set os.Environ.
func ParseFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	out := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for lineNum := 1; scanner.Scan(); lineNum++ {
		key, val, ok := parseLine(scanner.Text())
		if !ok {
			continue
		}
		out[key] = val
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return out, nil
}

func parseLine(line string) (key, val string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	if after, found := strings.CutPrefix(line, "export "); found {
		line = strings.TrimSpace(after)
	}
	key, val, found := strings.Cut(line, "=")
	if !found {
		return "", "", false
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", false
	}
	val = strings.TrimSpace(unquoteValue(val))
	return key, val, true
}

func unquoteValue(val string) string {
	if len(val) < 2 {
		return val
	}
	switch val[0] {
	case '"':
		if val[len(val)-1] == '"' {
			return strings.ReplaceAll(val[1:len(val)-1], `\"`, `"`)
		}
	case '\'':
		if val[len(val)-1] == '\'' {
			return val[1 : len(val)-1]
		}
	}
	return val
}

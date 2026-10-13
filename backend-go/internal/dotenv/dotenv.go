// Package dotenv loads a local .env file for the standalone process, the way
// the Node entry point did through dotenv/config. Variables already present in
// the environment always win, so container and shell configuration is never
// overridden by a file.
package dotenv

import (
	"bufio"
	"os"
	"strings"
)

// Load applies KEY=VALUE lines from filename. A missing file is not an error.
func Load(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
		key, value, ok := parseLine(scanner.Text())
		if !ok {
			continue
		}
		if _, exists := os.LookupEnv(key); !exists {
			if err := os.Setenv(key, value); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}

func parseLine(line string) (string, string, bool) {
	line = strings.TrimSpace(strings.TrimPrefix(line, string(rune(0xFEFF))))
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
	separator := strings.IndexByte(line, '=')
	if separator <= 0 {
		return "", "", false
	}
	key := strings.TrimSpace(line[:separator])
	for _, r := range key {
		if !(r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return "", "", false
		}
	}
	value := strings.TrimSpace(line[separator+1:])
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'' || value[0] == '`') {
		quote := value[0]
		if end := strings.LastIndexByte(value, quote); end > 0 {
			inner := value[1:end]
			if quote == '"' {
				inner = strings.NewReplacer(`\n`, "\n", `\r`, "\r", `\"`, `"`, `\\`, `\`).Replace(inner)
			}
			return key, inner, true
		}
	}
	// An unquoted value ends at an inline comment.
	if comment := strings.Index(value, " #"); comment >= 0 {
		value = strings.TrimSpace(value[:comment])
	}
	return key, value, true
}

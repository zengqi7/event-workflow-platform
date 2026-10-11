package migrations

import (
	"bufio"
	"embed"
	"fmt"
	"strings"
)

//go:embed *.up.sql
var Files embed.FS

// Statements parses the line-delimited SQL convention used by this repository,
// including MySQL client DELIMITER directives for compound triggers.
func Statements(raw string) ([]string, error) {
	var builder strings.Builder
	var statements []string
	delimiter := ";"
	scanner := bufio.NewScanner(strings.NewReader(raw))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "--") {
			continue
		}
		if strings.HasPrefix(line, "DELIMITER ") {
			if strings.TrimSpace(builder.String()) != "" {
				return nil, fmt.Errorf("delimiter changed mid-statement")
			}
			delimiter = strings.TrimSpace(strings.TrimPrefix(line, "DELIMITER "))
			if delimiter == "" {
				return nil, fmt.Errorf("empty delimiter")
			}
			continue
		}
		builder.WriteString(line)
		builder.WriteString("\n")
		if strings.HasSuffix(line, delimiter) {
			statements = append(statements, strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(builder.String()), delimiter)))
			builder.Reset()
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(builder.String()) != "" {
		return nil, fmt.Errorf("unterminated migration statement")
	}
	if len(statements) == 0 {
		return nil, fmt.Errorf("empty migration")
	}
	return statements, nil
}

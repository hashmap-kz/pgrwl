package optutils

import (
	"bufio"
	"strings"
)

func HeredocTrim(text string) string {
	sb := strings.Builder{}
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := scanner.Text()
		sb.WriteString(strings.TrimSpace(line))
		sb.WriteString("\n")
	}
	if err := scanner.Err(); err != nil {
		return text
	}
	return strings.TrimSpace(sb.String())
}

package strx

import (
	"bufio"
	"sort"
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

func SortDesc(topLevel map[string]bool) []string {
	r := []string{}
	for k := range topLevel {
		r = append(r, k)
	}
	sort.Slice(r, func(i, j int) bool {
		return r[i] > r[j] // Descending order
	})
	return r
}

func IsInList(id string, list []string) bool {
	for _, s := range list {
		if s == id {
			return true
		}
	}
	return false
}

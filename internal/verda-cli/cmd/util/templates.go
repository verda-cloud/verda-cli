package util

import "strings"

const indentation = "  "

// LongDesc normalises a command's long description by trimming leading/trailing
// whitespace and collapsing leading tabs (heredoc-style).
func LongDesc(s string) string {
	if s == "" {
		return s
	}
	return strings.TrimSpace(dedent(s))
}

// Examples normalises a command's examples by trimming whitespace and adding
// consistent 2-space indentation to each line.
func Examples(s string) string {
	if s == "" {
		return s
	}
	trimmed := strings.TrimSpace(dedent(s))
	lines := strings.Split(trimmed, "\n")
	indented := make([]string, len(lines))
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			indented[i] = ""
		} else {
			indented[i] = indentation + strings.TrimSpace(line)
		}
	}
	return strings.Join(indented, "\n")
}

func dedent(s string) string {
	lines := strings.Split(s, "\n")

	minIndent := -1
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		if minIndent < 0 || indent < minIndent {
			minIndent = indent
		}
	}
	if minIndent <= 0 {
		return s
	}

	result := make([]string, len(lines))
	for i, line := range lines {
		switch {
		case strings.TrimSpace(line) == "":
			result[i] = ""
		case len(line) >= minIndent:
			result[i] = line[minIndent:]
		default:
			result[i] = line
		}
	}
	return strings.Join(result, "\n")
}

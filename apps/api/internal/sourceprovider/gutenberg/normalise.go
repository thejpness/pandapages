package gutenberg

import (
	"bytes"
	"strings"
	"unicode/utf8"

	"pandapages/api/internal/sourceprovider"
)

const normalisationVersion = "project-gutenberg-plain-text-v1"

func normalisePlainText(content []byte) (string, error) {
	if !utf8.Valid(content) {
		return "", sourceprovider.ErrContentInvalid
	}
	if bytes.HasPrefix(content, []byte{0xef, 0xbb, 0xbf}) {
		content = content[3:]
	}
	text := normaliseLineEndings(content)
	lines := strings.Split(text, "\n")

	start, end := -1, -1
	for index, line := range lines {
		switch {
		case isGutenbergStartMarker(line):
			if start != -1 || end != -1 {
				return "", sourceprovider.ErrNormalisationFailed
			}
			start = index
		case isGutenbergEndMarker(line):
			if start == -1 || end != -1 {
				return "", sourceprovider.ErrNormalisationFailed
			}
			end = index
		}
	}
	if start == -1 || end == -1 || end <= start {
		return "", sourceprovider.ErrNormalisationFailed
	}

	body := lines[start+1 : end]
	for len(body) > 0 && strings.TrimSpace(body[0]) == "" {
		body = body[1:]
	}
	for len(body) > 0 && strings.TrimSpace(body[len(body)-1]) == "" {
		body = body[:len(body)-1]
	}
	if len(body) == 0 || strings.TrimSpace(strings.Join(body, "")) == "" {
		return "", sourceprovider.ErrNormalisationFailed
	}
	return strings.Join(body, "\n") + "\n", nil
}

func normaliseLineEndings(content []byte) string {
	if !bytes.Contains(content, []byte{'\r'}) {
		return string(content)
	}
	var normalised strings.Builder
	normalised.Grow(len(content))
	for index := 0; index < len(content); index++ {
		if content[index] != '\r' {
			normalised.WriteByte(content[index])
			continue
		}
		normalised.WriteByte('\n')
		if index+1 < len(content) && content[index+1] == '\n' {
			index++
		}
	}
	return normalised.String()
}

func isGutenbergStartMarker(line string) bool { return isGutenbergMarker(line, "START") }
func isGutenbergEndMarker(line string) bool   { return isGutenbergMarker(line, "END") }

func isGutenbergMarker(line, boundary string) bool {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "***") || !strings.HasSuffix(line, "***") {
		return false
	}
	middle := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "***"), "***"))
	words := strings.Fields(middle)
	if len(words) < 6 || !strings.EqualFold(words[0], boundary) || !strings.EqualFold(words[1], "OF") {
		return false
	}
	if strings.EqualFold(words[2], "THE") {
		return strings.EqualFold(words[3], "PROJECT") && strings.EqualFold(words[4], "GUTENBERG") && strings.EqualFold(words[5], "EBOOK")
	}
	return strings.EqualFold(words[2], "THIS") && len(words) >= 6 && strings.EqualFold(words[3], "PROJECT") && strings.EqualFold(words[4], "GUTENBERG") && strings.EqualFold(words[5], "EBOOK")
}

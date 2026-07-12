package storyingest

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestIngestPreservesUTF8PlainText(t *testing.T) {
	newline := string(rune(10))
	markdown := "# Café Panda 🐼" + newline + newline + "“Olá”, said the panda. 你好。" + newline

	out, err := Ingest(Input{
		Slug:     "cafe-panda",
		Title:    "Café Panda 🐼",
		Markdown: markdown,
	})
	if err != nil {
		t.Fatalf("Ingest returned error: %v", err)
	}
	if out.Markdown != markdown {
		t.Fatalf("markdown changed: got %q, want %q", out.Markdown, markdown)
	}
	if !utf8.ValidString(out.RenderedHTML) {
		t.Fatal("rendered HTML is not valid UTF-8")
	}
	for _, want := range []string{"Café Panda", "🐼", "Olá", "你好"} {
		if !strings.Contains(out.RenderedHTML, want) {
			t.Errorf("rendered HTML does not contain %q: %s", want, out.RenderedHTML)
		}
	}
	if len(out.Segments) != 2 {
		t.Fatalf("segments = %d, want 2", len(out.Segments))
	}
}

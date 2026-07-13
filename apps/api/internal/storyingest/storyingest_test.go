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

func TestIngestRejectsInvalidUTF8(t *testing.T) {
	_, err := Ingest(Input{
		Slug:     "invalid-utf8",
		Title:    "Invalid UTF-8",
		Markdown: string([]byte{'#', ' ', 0xff}),
	})
	if err == nil || !strings.Contains(err.Error(), "valid UTF-8") {
		t.Fatalf("error = %v, want invalid UTF-8 rejection", err)
	}
}

func TestIngestRejectsMalformedFrontmatter(t *testing.T) {
	tests := []string{
		"---\ntitle: [unterminated\n---\nStory",
		"---\ntitle: one\ntitle: two\n---\nStory",
		"---\ntitle: never closed",
	}
	for _, markdown := range tests {
		_, err := Ingest(Input{Slug: "bad-frontmatter", Title: "Bad frontmatter", Markdown: markdown})
		if err == nil {
			t.Fatalf("Ingest accepted malformed frontmatter %q", markdown)
		}
	}
}

func TestIngestOmitsUnsafeMarkdown(t *testing.T) {
	markdown := "# Safe title\n\n[click](&#106;avascript:alert(1))\n\n" +
		"<javascript:alert(document.domain)>\n\n" +
		"![alt](javascript:alert(document.domain))\n\n" +
		"<script>alert(1)</script><img src=x onerror=alert(1)>"
	out, err := Ingest(Input{Slug: "safe-rendering", Title: "Safe rendering", Markdown: markdown})
	if err != nil {
		t.Fatalf("Ingest returned error: %v", err)
	}

	rendered := strings.ToLower(out.RenderedHTML)
	for _, unsafe := range []string{`href="javascript:`, `src="javascript:`, "<script", "onerror"} {
		if strings.Contains(rendered, unsafe) {
			t.Errorf("rendered HTML contains unsafe content %q: %s", unsafe, out.RenderedHTML)
		}
	}
}

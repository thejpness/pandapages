package storyingest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"gopkg.in/yaml.v3"
)

var slugRe = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type Input struct {
	Slug     string
	Title    string
	Author   string
	Markdown string

	Language  string
	SourceURL string
	Rights    map[string]any
}

type Segment struct {
	Ordinal      int
	Locator      json.RawMessage
	Markdown     string
	RenderedHTML string
	WordCount    int
}

type Output struct {
	Slug        string
	Title       string
	Author      string
	Language    string
	Source      map[string]any
	Rights      map[string]any
	Frontmatter map[string]any

	Markdown     string
	RenderedHTML string
	ContentHash  string

	Segments []Segment
}

func ValidateSlug(slug string) error {
	if !slugRe.MatchString(slug) {
		return fmt.Errorf("invalid slug: use lowercase letters/numbers/hyphens (e.g. the-gruffalo)")
	}
	return nil
}

// Parse optional YAML frontmatter --- ... ---
func splitFrontmatter(md string) (fm map[string]any, body string) {
	s := strings.TrimLeft(md, "\ufeff \t\r\n")
	if !strings.HasPrefix(s, "---\n") && !strings.HasPrefix(s, "---\r\n") {
		return map[string]any{}, md
	}
	// find closing '---' on its own line
	lines := strings.Split(s, "\n")
	if len(lines) < 3 {
		return map[string]any{}, md
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return map[string]any{}, md
	}

	fmText := strings.Join(lines[1:end], "\n")
	body = strings.Join(lines[end+1:], "\n")

	out := map[string]any{}
	_ = yaml.Unmarshal([]byte(fmText), &out)
	return out, body
}

func render(md string) (string, error) {
	mdr := goldmark.New(
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	)
	var buf bytes.Buffer
	if err := mdr.Convert([]byte(md), &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func wordCount(s string) int {
	return len(strings.Fields(strings.ReplaceAll(s, "\n", " ")))
}

func extractBlockSource(src []byte, n ast.Node) string {
	type liner interface{ Lines() *text.Segments }
	l, ok := n.(liner)
	if !ok || l.Lines() == nil || l.Lines().Len() == 0 {
		return ""
	}

	var b strings.Builder
	segs := l.Lines()
	for i := 0; i < segs.Len(); i++ {
		seg := segs.At(i)
		part := src[seg.Start:seg.Stop]
		b.Write(part)
	}
	return strings.TrimSpace(b.String())
}

func textContent(src []byte, n ast.Node) string {
	var b strings.Builder
	var walk func(ast.Node)
	walk = func(x ast.Node) {
		for c := x.FirstChild(); c != nil; c = c.NextSibling() {
			if t, ok := c.(*ast.Text); ok {
				seg := t.Segment
				b.Write(src[seg.Start:seg.Stop])
			}
			walk(c)
		}
	}
	walk(n)
	return strings.TrimSpace(b.String())
}

func Ingest(in Input) (Output, error) {
	in.Slug = strings.TrimSpace(in.Slug)
	in.Title = strings.TrimSpace(in.Title)
	in.Author = strings.TrimSpace(in.Author)

	if in.Title == "" {
		return Output{}, fmt.Errorf("title is required")
	}
	if in.Slug == "" {
		return Output{}, fmt.Errorf("slug is required")
	}
	if err := ValidateSlug(in.Slug); err != nil {
		return Output{}, err
	}
	if strings.TrimSpace(in.Markdown) == "" {
		return Output{}, fmt.Errorf("markdown is required")
	}

	fm, body := splitFrontmatter(in.Markdown)

	// prefer explicit fields, fall back to frontmatter
	if v, ok := fm["title"].(string); in.Title == "" && ok {
		in.Title = strings.TrimSpace(v)
	}
	if v, ok := fm["author"].(string); in.Author == "" && ok {
		in.Author = strings.TrimSpace(v)
	}
	if v, ok := fm["language"].(string); in.Language == "" && ok {
		in.Language = strings.TrimSpace(v)
	}
	if v, ok := fm["sourceUrl"].(string); in.SourceURL == "" && ok {
		in.SourceURL = strings.TrimSpace(v)
	}

	if in.Language == "" {
		in.Language = "en-GB"
	}
	if in.Rights == nil {
		in.Rights = map[string]any{}
	}

	// full render
	fullHTML, err := render(body)
	if err != nil {
		return Output{}, err
	}

	sum := sha256.Sum256([]byte(body))
	hash := hex.EncodeToString(sum[:])

	// AST segmentation (blocks)
	mdr := goldmark.New()
	reader := text.NewReader([]byte(body))
	doc := mdr.Parser().Parse(reader)

	src := []byte(body)
	segs := make([]Segment, 0, 64)
	ordinal := 1
	paraN := 0
	headIdx := 0

	for n := doc.FirstChild(); n != nil; n = n.NextSibling() {
		switch x := n.(type) {
		case *ast.Heading:
			txt := textContent(src, x)
			if txt == "" {
				txt = extractBlockSource(src, x)
			}
			level := x.Level
			md := strings.Repeat("#", level) + " " + txt
			h, _ := render(md)
			loc, _ := json.Marshal(map[string]any{"type": "heading", "h": level, "index": headIdx})
			headIdx++

			segs = append(segs, Segment{
				Ordinal: ordinal, Locator: loc, Markdown: md, RenderedHTML: h, WordCount: wordCount(txt),
			})
			ordinal++

		case *ast.Paragraph:
			md := extractBlockSource(src, x)
			if md == "" {
				md = textContent(src, x)
			}
			paraN++
			h, _ := render(md)
			loc, _ := json.Marshal(map[string]any{"type": "para", "n": paraN})

			segs = append(segs, Segment{
				Ordinal: ordinal, Locator: loc, Markdown: md, RenderedHTML: h, WordCount: wordCount(md),
			})
			ordinal++

		default:
			// fallback: try to preserve original block text if possible
			md := extractBlockSource(src, n)
			if strings.TrimSpace(md) == "" {
				continue
			}
			h, _ := render(md)
			loc, _ := json.Marshal(map[string]any{"type": "block", "kind": fmt.Sprintf("%T", n)})

			segs = append(segs, Segment{
				Ordinal: ordinal, Locator: loc, Markdown: md, RenderedHTML: h, WordCount: wordCount(md),
			})
			ordinal++
		}
	}

	source := map[string]any{}
	if strings.TrimSpace(in.SourceURL) != "" {
		source["url"] = strings.TrimSpace(in.SourceURL)
	}

	frontmatter := map[string]any{
		"title":    in.Title,
		"author":   in.Author,
		"language": in.Language,
	}
	if u := strings.TrimSpace(in.SourceURL); u != "" {
		frontmatter["sourceUrl"] = u
	}

	// merge fm → frontmatter (but keep explicit fields authoritative)
	for k, v := range fm {
		if _, exists := frontmatter[k]; !exists {
			frontmatter[k] = v
		}
	}

	return Output{
		Slug:        in.Slug,
		Title:       in.Title,
		Author:      in.Author,
		Language:    in.Language,
		Source:      source,
		Rights:      in.Rights,
		Frontmatter: frontmatter,

		Markdown:     body,
		RenderedHTML: fullHTML,
		ContentHash:  hash,
		Segments:     segs,
	}, nil
}

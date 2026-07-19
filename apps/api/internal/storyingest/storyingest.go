package storyingest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html"
	"regexp"
	"strings"
	"unicode/utf8"

	"pandapages/api/internal/readercontract"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"go.yaml.in/yaml/v3"
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
	Ordinal           int
	Kind              readercontract.SegmentKind
	HeadingLevel      *int
	ContentKey        string
	ContentOccurrence int
	ChapterKey        *string
	ChapterOccurrence *int
	Markdown          string
	RenderedHTML      string
	WordCount         int
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

const maxFrontmatterBytes = 64 << 10 // 64 KiB

// Parse optional YAML frontmatter --- ... ---.
func splitFrontmatter(md string) (fm map[string]any, body string, err error) {
	s := strings.TrimLeft(md, "\ufeff \t\r\n")
	if !strings.HasPrefix(s, "---\n") && !strings.HasPrefix(s, "---\r\n") {
		return map[string]any{}, md, nil
	}

	lines := strings.Split(s, "\n")
	if len(lines) < 3 {
		return nil, "", fmt.Errorf("frontmatter is not closed")
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return nil, "", fmt.Errorf("frontmatter is not closed")
	}

	fmText := strings.Join(lines[1:end], "\n")
	if len(fmText) > maxFrontmatterBytes {
		return nil, "", fmt.Errorf("frontmatter exceeds %d bytes", maxFrontmatterBytes)
	}
	body = strings.Join(lines[end+1:], "\n")

	out := map[string]any{}
	if err := yaml.Unmarshal([]byte(fmText), &out); err != nil {
		return nil, "", fmt.Errorf("invalid YAML frontmatter: %w", err)
	}
	return out, body, nil
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

func hasReadableRenderedText(rendered string) bool {
	var text strings.Builder
	inTag := false
	for _, character := range rendered {
		switch {
		case !inTag && character == '<':
			inTag = true
		case inTag && character == '>':
			inTag = false
		case !inTag:
			text.WriteRune(character)
		}
	}
	return strings.TrimSpace(html.UnescapeString(text.String())) != ""
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

func validateUTF8(in Input) error {
	values := []struct {
		name  string
		value string
	}{
		{name: "slug", value: in.Slug},
		{name: "title", value: in.Title},
		{name: "author", value: in.Author},
		{name: "markdown", value: in.Markdown},
		{name: "language", value: in.Language},
		{name: "source URL", value: in.SourceURL},
	}
	for _, field := range values {
		if !utf8.ValidString(field.value) {
			return fmt.Errorf("%s is not valid UTF-8", field.name)
		}
	}
	return nil
}

func Ingest(in Input) (Output, error) {
	return ingest(in, false, nil)
}

// CanonicalizeStoredBody applies the same rendering, segmentation, and Reader
// identity contract as Ingest to a story-version body whose outer frontmatter
// has already been removed. Stored bodies must not be reparsed for frontmatter:
// a legitimate body can itself begin with a thematic break.
func CanonicalizeStoredBody(in Input, frontmatter map[string]any) (Output, error) {
	return ingest(in, true, frontmatter)
}

func ingest(in Input, bodyAlreadySplit bool, presetFrontmatter map[string]any) (Output, error) {
	if err := validateUTF8(in); err != nil {
		return Output{}, err
	}

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

	fm := map[string]any{}
	body := in.Markdown
	if bodyAlreadySplit {
		for key, value := range presetFrontmatter {
			fm[key] = value
		}
	} else {
		var err error
		fm, body, err = splitFrontmatter(in.Markdown)
		if err != nil {
			return Output{}, err
		}
	}

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
			headingLevel := level

			segs = append(segs, Segment{
				Ordinal: ordinal, Kind: readercontract.SegmentKindHeading, HeadingLevel: &headingLevel,
				Markdown: md, RenderedHTML: h, WordCount: wordCount(txt),
			})
			ordinal++

		case *ast.Paragraph:
			md := extractBlockSource(src, x)
			if md == "" {
				md = textContent(src, x)
			}
			h, _ := render(md)

			segs = append(segs, Segment{
				Ordinal: ordinal, Kind: readercontract.SegmentKindParagraph,
				Markdown: md, RenderedHTML: h, WordCount: wordCount(md),
			})
			ordinal++

		default:
			// fallback: try to preserve original block text if possible
			md := extractBlockSource(src, n)
			if strings.TrimSpace(md) == "" {
				continue
			}
			h, _ := render(md)

			segs = append(segs, Segment{
				Ordinal: ordinal, Kind: readercontract.SegmentKindOther,
				Markdown: md, RenderedHTML: h, WordCount: wordCount(md),
			})
			ordinal++
		}
	}
	if len(segs) == 0 {
		return Output{}, fmt.Errorf("story must contain at least one readable segment")
	}
	hasReadableSegment := false
	for _, segment := range segs {
		if hasReadableRenderedText(segment.RenderedHTML) {
			hasReadableSegment = true
			break
		}
	}
	if !hasReadableSegment {
		return Output{}, fmt.Errorf("story must contain at least one readable segment")
	}

	identityInputs := make([]readercontract.SegmentIdentityInput, 0, len(segs))
	for _, segment := range segs {
		identityInputs = append(identityInputs, readercontract.SegmentIdentityInput{
			Kind:         segment.Kind,
			HeadingLevel: segment.HeadingLevel,
			Markdown:     segment.Markdown,
		})
	}
	identities, err := readercontract.AssignSegmentIdentities(identityInputs)
	if err != nil {
		return Output{}, err
	}
	for index := range segs {
		segs[index].ContentKey = identities[index].ContentKey
		segs[index].ContentOccurrence = identities[index].ContentOccurrence
		segs[index].ChapterKey = identities[index].ChapterKey
		segs[index].ChapterOccurrence = identities[index].ChapterOccurrence
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

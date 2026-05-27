package web

import (
	"bytes"
	"fmt"
	"html"
	"regexp"
	"strings"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
)

var (
	markdownRenderer = goldmark.New(
		goldmark.WithExtensions(
			highlighting.NewHighlighting(
				highlighting.WithFormatOptions(
					chromahtml.WithClasses(true),
				),
			),
		),
	)
	yamlHTMLPolicy = newYAMLHTMLPolicy()
)

func newYAMLHTMLPolicy() *bluemonday.Policy {
	policy := bluemonday.StrictPolicy()
	policy.AllowElements("div", "pre", "code", "span")
	policy.AllowAttrs("class").Matching(regexp.MustCompile(`^[A-Za-z0-9_\-\s]+$`)).OnElements("div", "pre", "code", "span")
	return policy
}

func renderYAMLHTML(yaml string) string {
	markdown := fencedCodeBlock("yaml", yaml)
	var rendered bytes.Buffer
	if err := markdownRenderer.Convert([]byte(markdown), &rendered); err != nil {
		return yamlHTMLPolicy.Sanitize(fmt.Sprintf("<pre><code>%s</code></pre>", html.EscapeString(yaml)))
	}
	return yamlHTMLPolicy.Sanitize(rendered.String())
}

func fencedCodeBlock(language string, content string) string {
	fence := "```"
	for strings.Contains(content, fence) {
		fence += "`"
	}
	return fence + language + "\n" + content + "\n" + fence + "\n"
}

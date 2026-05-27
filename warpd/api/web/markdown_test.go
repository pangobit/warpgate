package web

import (
	"strings"
	"testing"
)

func TestRenderYAMLHTMLHighlightsAndSanitizes(t *testing.T) {
	html := renderYAMLHTML("kind: warpgate/app\nunsafe: <script>alert(1)</script>\n")

	if !strings.Contains(html, `class="chroma"`) {
		t.Fatalf("expected highlighted YAML HTML, got %s", html)
	}
	if !strings.Contains(html, `class="nt"`) {
		t.Fatalf("expected YAML key highlighting, got %s", html)
	}
	if strings.Contains(html, "<script>") {
		t.Fatalf("expected script tag to be sanitized, got %s", html)
	}
	if !strings.Contains(html, "alert(1)") {
		t.Fatalf("expected sanitized YAML text to remain visible, got %s", html)
	}
}

func TestFencedCodeBlockHandlesBackticksInYAML(t *testing.T) {
	block := fencedCodeBlock("yaml", "value: ```")

	if !strings.HasPrefix(block, "````yaml\n") {
		t.Fatalf("expected fence to expand past content backticks, got %q", block)
	}
	if !strings.HasSuffix(block, "\n````\n") {
		t.Fatalf("expected closing fence to match expanded opening fence, got %q", block)
	}
}

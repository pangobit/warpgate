package web

import "github.com/a-h/templ"

const yamlDocumentCSS = `.yamlDocument {
	white-space: normal;
	line-height: 1.45;
}

.yamlDocument .chroma,
.yamlDocument pre,
.yamlDocument code {
	margin: 0;
	background: transparent;
	font-family: var(--font-mono);
}

.yamlDocument pre {
	overflow: visible;
	white-space: pre;
}

.yamlDocument .nt {
	color: var(--accent);
	font-weight: 800;
}

.yamlDocument .s,
.yamlDocument .s1,
.yamlDocument .s2 {
	color: var(--success);
}

.yamlDocument .m,
.yamlDocument .mi,
.yamlDocument .mf,
.yamlDocument .kc {
	color: var(--warning);
}

.yamlDocument .c,
.yamlDocument .c1 {
	color: var(--text-soft);
}`

func yamlDocument() templ.CSSClass {
	return templ.ComponentCSSClass{
		ID:    "yamlDocument",
		Class: templ.SafeCSS(yamlDocumentCSS),
	}
}

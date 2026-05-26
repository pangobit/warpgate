package web

import "github.com/a-h/templ"

// ComponentStyles returns templ CSS components used by the web UI.
func ComponentStyles() []templ.CSSClass {
	return []templ.CSSClass{
		appFrame(),
		sidebar(),
		brand(),
		brandMark(),
		sidebarNav(),
		sidebarLink(),
		navIcon(),
		pillIcon(),
		buttonIcon(),
		sidebarFooter(),
		appMain(),
		dashboardGrid(),
		managementShell(),
		detailGrid(),
		panel(),
		sectionHeading(),
		pageTitle(),
		panelTitle(),
		eyebrow(),
		primaryButton(),
		iconButton(),
		pill(),
		identityPill(),
		statusBase(),
		statusSuccess(),
		statusWarning(),
		statusDanger(),
		field(),
		control(),
		formActions(),
		tableWrap(),
		tableView(),
		tableCell(),
		tableHead(),
		codeText(),
		preBlock(),
		errorBox(),
	}
}

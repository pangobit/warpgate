# Warpgate Web Design System

## Purpose

This document defines the Warpgate web UI design system. It is self-contained so future implementation work does not require access to any other repository.

The UI should feel operational, quiet, and dense enough for repeated deployment work. Prefer clear tables, compact panels, status indicators, and direct actions over marketing-style layouts.

## Frontend Stack

- Use `templ` for HTML templates.
- Use HTMX for form submissions, partial swaps, and optional event streams.
- Use vanilla CSS in one embedded stylesheet.
- Use minimal JavaScript for theme toggling and keyboard navigation only.
- Embed CSS and JavaScript assets in the Go binary.
- Do not add a frontend framework.

## Visual Tokens

Use CSS custom properties as the source of truth. Start with these tokens and extend only when a component needs a semantic color or spacing value that is not represented.

```css
:root {
  --font-sans: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
  --font-mono: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace;
  --space-1: 0.25rem;
  --space-2: 0.5rem;
  --space-3: 0.75rem;
  --space-4: 1rem;
  --space-5: 1.5rem;
  --space-6: 2rem;
  --radius-1: 0.25rem;
  --radius-2: 0.5rem;
  --radius-pill: 999px;
  --bg-page: #ebe9e4;
  --bg-panel: #f5f4f1;
  --bg-surface: #ffffff;
  --bg-subtle: #efece7;
  --bg-hover: #eae8e2;
  --bg-command: #ebf0ea;
  --bg-error: #f9f0f0;
  --bg-action: #2e2d2b;
  --bg-terminal: #2e2d2b;
  --text-main: #2d2d2d;
  --text-muted: #6d6a65;
  --text-soft: #8a8a8a;
  --text-command: #4a5d4e;
  --text-mono: #5c5c5c;
  --text-inverse: #ffffff;
  --border-subtle: #e2dfd8;
  --border-strong: #d4d0c6;
  --accent: #477c54;
  --accent-strong: #2e2d2b;
  --success-bg: #e4ede6;
  --success: #477c54;
  --warning-bg: #fdf4e5;
  --warning: #c78b31;
  --danger-bg: #fce9e9;
  --danger: #c94a4a;
  --shadow-panel: 0 0.5rem 1.5rem rgb(46 45 43 / 0.07);
}

html.dark {
  --bg-page: #171612;
  --bg-panel: #20201d;
  --bg-surface: #292823;
  --bg-subtle: #2f2d28;
  --bg-hover: #38362f;
  --bg-command: #243027;
  --bg-error: #372426;
  --bg-action: #f5f4f1;
  --bg-terminal: #12110f;
  --text-main: #f5f4f1;
  --text-muted: #c6c1b8;
  --text-soft: #a49f96;
  --text-command: #b8d1bd;
  --text-mono: #d7d1c6;
  --text-inverse: #2e2d2b;
  --border-subtle: #413e36;
  --border-strong: #5a554b;
  --accent: #8fbd98;
  --accent-strong: #f5f4f1;
  --success-bg: #213527;
  --success: #8fbd98;
  --warning-bg: #3a2d18;
  --warning: #e6bc72;
  --danger-bg: #3d2427;
  --danger: #ed9b9b;
  --shadow-panel: none;
}
```

## Global Rules

- Apply `box-sizing: border-box` globally.
- Body background uses `--bg-page`; foreground uses `--text-main`.
- Body font is `--font-sans`, `1rem`, `line-height: 1.5`.
- All interactive elements use inherited font.
- Use `:focus-visible` with a `0.18rem` solid `--accent` outline and `0.15rem` offset.
- Use short transitions for background, color, border, and shadow changes.
- Keep border radii small: `--radius-1` for compact chips and marks, `--radius-2` for panels and controls.

## App Shell

Use a two-column shell:

- `.app-frame`: CSS grid with `14rem minmax(0, 1fr)`, minimum height `100vh`.
- `.sidebar`: sticky left navigation, full viewport height, `--bg-panel`, right border, `--space-4` padding.
- `.app-main`: centered content, `width: min(92rem, 100%)`, `--space-5` padding.

Sidebar elements:

- `.brand`: inline flex, bold, no underline.
- `.brand-mark`: 1.5rem square, `--bg-action`, inverse text, `--radius-1`.
- `.sidebar-nav`: grid with `--space-1` gap.
- `.sidebar-link`: 2.25rem minimum height, muted text, `--radius-2`, hover background `--bg-hover`.
- `.nav-indicator`: small accent dot with reduced opacity until hover/focus.
- `.sidebar-footer`: system status pill and theme button.

The Warpgate brand mark should use `W`.

## Layout Grids

Use these layout patterns:

- `.dashboard-grid`: two columns, `minmax(20rem, 0.8fr) minmax(0, 1.4fr)`, `--space-5` gap.
- `.management-shell`: two columns, `minmax(20rem, 0.75fr) minmax(0, 1.25fr)`, `--space-5` gap.
- `.detail-grid`: for app/release detail pages, one wide content column plus one narrow side column.

All grid containers must set `min-width: 0` to prevent long image refs, commit SHAs, and domains from overflowing.

## Panels

Use `.panel` for framed work surfaces:

- `background: var(--bg-surface)`
- `border: 1px solid var(--border-subtle)`
- `border-radius: var(--radius-2)`
- `box-shadow: var(--shadow-panel)`
- `padding: var(--space-5)`
- `min-width: 0`

Do not nest panels inside panels. Use sections, fieldsets, rows, or subtle backgrounds inside a panel.

## Section Headers

Use `.section-heading` at the top of panels:

- flex row
- align top
- space-between
- `--space-3` gap
- `--space-4` bottom margin

Heading scale:

- `h1`: `1.5rem`
- `h2`: `1.08rem`
- compact card titles: `1rem`

Use `.eyebrow` for metadata labels:

- uppercase
- `0.68rem`
- `font-weight: 900`
- `letter-spacing: 0.08em`
- `color: var(--text-soft)`

## Buttons

Use `.primary-button` for the main form/action button:

- minimum height `2.5rem`
- horizontal padding `--space-4`
- background `--bg-action`
- text `--text-inverse`
- border `--bg-action`
- `font-weight: 800`
- hover/focus background `--accent`

Use `.icon-button` for icon-only or compact utility actions:

- 2.5rem square
- grid center
- background `--bg-subtle`
- border `--border-subtle`
- hover/focus `--bg-hover` and `--border-strong`

Use text labels for high-risk actions such as deploy and commit. Icon-only buttons are appropriate for refresh/check-now, theme toggle, copy, and open-link actions when accessible labels are present.

## Pills and Status

Use `.system-pill` and `.count-badge` for compact metadata:

- inline flex
- min height `1.75rem`
- horizontal padding `--space-2`
- pill radius
- bold `0.78rem`
- no wrapping

Use `.status` for lifecycle state:

- inline flex
- min height `1.6rem`
- border uses current color
- pill radius
- uppercase `0.68rem`
- `font-weight: 900`
- `letter-spacing: 0.08em`

Map statuses:

- Success states: `deployed`, `succeeded`, `ready`, `healthy`
- Warning states: `draft`, `queued`, `running`, `deploying`, `pending`, `changed`
- Danger states: `failed`, `invalid`, `blocked`, `unhealthy`

Use token pairs:

- success: `--success`, `--success-bg`
- warning: `--warning`, `--warning-bg`
- danger: `--danger`, `--danger-bg`

## Forms

Use `.field` labels:

- grid layout
- `--space-2` gap
- label text `0.86rem`, muted, `font-weight: 800`

Controls:

- full width
- text `--text-main`
- background `--bg-panel`
- border `--border-subtle`
- radius `--radius-2`
- padding `--space-2 --space-3`

Textareas:

- minimum height `10rem`
- vertical resize
- use monospace only for YAML or command previews

Use `.field-inline` for a label plus a compact right-side input with grid columns `1fr 8rem`.

Use `.form-actions` to align submit buttons to the right.

## Tables

Tables are preferred for app lists, release lists, deployment history, and refresh history.

Use:

- `.table-wrap` with horizontal overflow
- full-width collapsed table
- `th` and `td` padding `--space-3`
- row border `--border-subtle`
- uppercase `th` labels at `0.72rem`
- muted body text
- hover row background `--bg-subtle`

Long cells such as image refs, commit SHAs, and domains should use wrapping helpers:

- `overflow-wrap: anywhere`
- monospace for hashes, image refs, and paths

## Inline Lists and Code

Use `.inline-list` for compact tag lists such as services, nodes, domains, and capabilities:

- flex wrap
- `--space-2` gap
- no bullets
- items use monospace `0.84rem`
- item background `--bg-command`
- item text `--text-command`
- radius `--radius-1`

Use `code` for commit SHAs, image refs, node IDs, file paths, and env var names.

## Warpgate-Specific Surfaces

Dashboard should include:

- repository sync panel with last commit, last check, and check-now action
- app count
- latest deployment status
- image update summary

App detail should include:

- release services table
- target nodes
- current config commit
- current YAML preview
- release history
- deployment history

Deploy-data edit should include:

- structured fields for service image tags and digests
- structured fields for targets, strategy, routing, and environment
- generated YAML preview
- validation errors near the relevant field and in a summary
- commit preview and commit action

Settings should include:

- GitHub account connection state
- attached GitHub repo
- branch
- deploy SSH mode

## Template Shape

Use a single layout template with this structure:

```templ
templ Layout(title string) {
  <!DOCTYPE html>
  <html lang="en">
    <head>
      <meta charset="UTF-8"/>
      <meta name="viewport" content="width=device-width, initial-scale=1.0"/>
      <title>{ title }</title>
      <link rel="stylesheet" href="/assets/css/style.css"/>
      <script src="/assets/js/htmx.min.js"></script>
      <script src="/assets/js/keyboard.js" defer></script>
    </head>
    <body>
      <div class="app-frame">
        <aside class="sidebar" aria-label="Workspace">
          <a class="brand" href="/" data-navigable>
            <span class="brand-mark" aria-hidden="true">W</span>
            <span>Warpgate</span>
          </a>
          <nav class="sidebar-nav" aria-label="Primary">
            <!-- sidebar links -->
          </nav>
          <div class="sidebar-footer">
            <!-- system pill and theme button -->
          </div>
        </aside>
        <main class="app-main" id="main" tabindex="-1">
          { children... }
        </main>
      </div>
    </body>
  </html>
}
```

Use `data-navigable` on primary links, buttons, and form controls that keyboard navigation should visit.

## Responsive Behavior

The first implementation can use the desktop shell specified above, but must not break on narrow screens:

- Switch `.app-frame` to one column on small screens.
- Make `.sidebar` non-sticky or horizontally scrollable on small screens.
- Collapse two-column grids to one column below a practical breakpoint.
- Preserve access to deploy and commit actions without horizontal scrolling.

## Accessibility

- Every form control needs a visible label.
- Every icon-only button needs an `aria-label`.
- Status pills should include text, not color alone.
- Tables should use `th scope="col"`.
- Generated YAML previews should be readable by keyboard users and copyable.
- Do not rely on hover-only controls for deploy, commit, or check-now actions.

## Copy Rules

- Use concrete operational labels: `Check now`, `Commit release`, `Deploy release`, `Last checked`, `Current digest`, `Config commit`.
- Avoid explanatory marketing copy.
- Keep error messages short and actionable.
- Prefer exact field names when validation fails, such as `release.services.api.image_tag is required`.

## Implementation Rule

The Warpgate stylesheet and templates should be local files in this repository. Do not import assets or packages from another repository at runtime. If a future change needs to diverge from this design system, update this document in the same change.

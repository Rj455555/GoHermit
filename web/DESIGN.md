# GoHermit Workbench Design System

## 1. Visual theme

GoHermit is a focused personal operations console: calm, precise, and dense enough
for daily use without looking like an infrastructure monitoring product. The visual
reference is a well-made desktop coding tool rather than a marketing dashboard.

- Density: balanced workbench, 6/10.
- Variance: asymmetric primary/secondary panels, 5/10.
- Motion: restrained state transitions, 3/10.
- Surfaces communicate hierarchy; they are not used to wrap every paragraph.

## 2. Color roles

- Canvas Stone `#F3F2EE`: application background.
- Paper Surface `#FCFCFA`: forms, panels, and raised content.
- Soft Stone `#EBE9E3`: selected neutral controls and secondary surfaces.
- Charcoal Ink `#25231F`: primary text.
- Muted Graphite `#6D6961`: descriptions and metadata.
- Structural Border `#DEDBD2`: quiet layout boundaries.
- Fired Clay `#C45A3A`: the single accent for primary actions, progress, and focus.
- Success Pine `#2C7658`: successful readiness.
- Danger Brick `#B83A32`: blocking failures and destructive actions.
- Navigation Charcoal `#242522`: persistent left navigation.

No neon, purple glow, decorative gradient, or fabricated metric is allowed.

## 3. Typography

- UI: Geist when available, then Aptos and the operating-system sans-serif.
- Code, identifiers, counts, and timestamps: Geist Mono, SFMono-Regular, or Consolas.
- Page headings use restrained sizes and tight tracking; hierarchy comes from weight,
  spacing, and contrast.
- Body text stays below 65 characters per line where it is prose.

## 4. Layout

- Expanded navigation is 228 px; collapsed navigation is 68 px.
- Main content is bounded to 1360 px and uses fluid outer padding.
- Dashboard uses one strong workspace surface, a compact status strip, and an
  asymmetric content split.
- Employee lists use responsive resource rows/cards, never empty white canvases.
- Forms are one column on mobile and no more than two columns on desktop.
- Touch targets are at least 40 px desktop and 44 px for primary form controls.

## 5. Component behavior

- Buttons have primary, secondary, and destructive hierarchy with tactile active state.
- Inputs show a clay focus ring and keep labels above the field.
- Loading states preserve the target layout; error states include a direct recovery action.
- Empty states explain what the resource is and provide one primary next action.
- The Employee wizard validates each phase before advancing and shows all nine phases
  as an explicit progress track.
- Partial projection failures retain usable authoritative data instead of replacing the
  entire page with a generic error.

## 6. Accessibility and motion

- Keyboard focus is always visible.
- Navigation collapse and dialogs retain the existing focus-restoration contracts.
- Status is never communicated by color alone.
- `prefers-reduced-motion` disables non-essential transitions.
- Responsive layouts must not create horizontal page scrolling; only bounded controls
  such as the nine-step progress track may scroll internally.

## 7. Banned patterns

- No equal three-card marketing rows as the primary layout.
- No default browser buttons in product surfaces.
- No raw JSON as the normal user-facing representation.
- No invented performance, uptime, or productivity statistics.
- No emoji decoration, outer glow, huge centered hero, or filler copy.
- No generic error that destroys already loaded data.

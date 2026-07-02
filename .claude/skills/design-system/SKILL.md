---
name: design-system
description: The visual language of the Baseline dashboard across web (React/CSS tokens) and Android (Compose). Use this skill whenever building or modifying ANY UI in this project — new cards, charts, screens, empty states, or copy — even small tweaks, so the two platforms stay one product. Also use when the user says something looks off, asks for a new dashboard panel, mentions colors/fonts/spacing, or requests a redesign.
---

# Baseline design system

## The one idea

The signature element is the **personal baseline band**: every value is shown against
the user's own 30-day norm (±1σ band on charts, z-deviation text and dots on cards).
Color exists ONLY to encode deviation. If a proposed element uses color decoratively,
it's wrong for this product.

## Tokens (canonical: web/src/design/tokens.css; mirrored in DashboardScreen.kt)

Surfaces: ink #0C1116 (page), panel #131A21, panel-edge #1E2731, band #22303B.
Text: #E7ECF0, dim #8B98A5, faint #566270.
Semantics — the only expressive colors:
- sage #8FBCA8 = favorable deviation / value line
- ember #E0785C = deviation needing attention
Type: Space Grotesk (display/headings), Inter (body), JetBrains Mono (ALL numerals,
with tabular-nums — class `.num` on web, FontFamily.Monospace in Compose).
Radius 10px; card padding ~18px web / 14dp Compose; grid gap 20px / 12dp.

Never introduce a new color or typeface. If a third semantic state is truly needed,
raise it as a design decision, don't improvise a hex.

## Component rules

- **Metric card**: uppercase 11–12px dim label → mono value with unit → deviation line
  ("+4 vs your 30-day norm (+0.8σ)", colored by zClass) → 14 deviation dots.
- **zClass semantics** (must match baseline-stats skill): |z|<0.75 neutral/dim; else
  sage/ember by the metric's higherIsBetter direction; null direction → ember.
- **Trend chart**: band area (band color, no stroke) + dashed mean30 line (faint) +
  value line (sage, 2px). No gridlines, no legends, axes in faint mono 11px.
  Animations off (isAnimationActive={false}) — instrument, not showpiece.
- **Empty/building states**: instructive, not apologetic. "Building baseline…" /
  "No data yet. Grant Health Connect access and let the first sync run."
- **Copy**: sentence case, plain verbs, no exclamation marks, no wellness-speak
  ("optimize", "crush", "journey" are banned). The product voice is a quiet instrument.

## Accessibility floor (non-negotiable)

Visible focus (2px sage outline, offset 2px), prefers-reduced-motion respected
(already global in tokens.css), text contrast ≥ 4.5:1 against panel, hit targets
≥ 44px on touch. Sage/ember must never be the ONLY signal — deviation text always
accompanies dot/line color (red-green colorblind safety).

## When adding a web component

Use existing CSS custom properties; no inline hex, no CSS-in-JS, no new dependencies
for styling. Charts: Recharts for standard panels; hand-rolled SVG/D3 only for a
genuinely new visualization, and only after the Recharts version proves insufficient.

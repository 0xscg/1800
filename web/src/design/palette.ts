/**
 * Bridge between design/tokens.css and Recharts, which needs concrete color
 * strings (SVG presentation attributes don't resolve var()). Values are read
 * from the live stylesheet so tokens.css stays the single source of truth;
 * the fallbacks mirror it exactly for non-DOM contexts.
 */
const FALLBACK: Record<string, string> = {
  ink: "#0c1116",
  panel: "#131a21",
  "panel-edge": "#1e2731",
  text: "#e7ecf0",
  "text-dim": "#8b98a5",
  "text-faint": "#566270",
  sage: "#8fbca8",
  ember: "#e0785c",
  band: "#22303b",
};

export function token(name: keyof typeof FALLBACK & string): string {
  if (typeof document !== "undefined") {
    const v = getComputedStyle(document.documentElement)
      .getPropertyValue(`--${name}`)
      .trim();
    if (v) return v;
  }
  return FALLBACK[name];
}

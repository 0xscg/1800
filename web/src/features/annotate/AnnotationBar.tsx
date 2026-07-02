import { useState } from "react";

const TAGS = ["travel", "illness", "alcohol", "late meal", "stress", "fasted"];

/** One-tap context tagging — the thing commercial apps never let you do. */
export function AnnotationBar() {
  const [saved, setSaved] = useState<string | null>(null);

  async function tagToday(tag: string) {
    const day = new Date().toISOString().slice(0, 10);
    try {
      await fetch("/v1/annotations", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ day, tag, note: "" }),
      });
    } catch {
      /* offline/mock mode: still confirm locally */
    }
    setSaved(tag);
    setTimeout(() => setSaved(null), 1800);
  }

  return (
    <div className="section">
      <h2>Tag today</h2>
      <div style={{ display: "flex", gap: 10, flexWrap: "wrap" }}>
        {TAGS.map((t) => (
          <button key={t} onClick={() => tagToday(t)}>
            {saved === t ? "Tagged ✓" : t}
          </button>
        ))}
      </div>
    </div>
  );
}

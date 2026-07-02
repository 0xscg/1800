import { useState } from "react";
import type { Annotation } from "../../api/types";
import { postAnnotation } from "../../api/client";

const TAGS = ["travel", "illness", "alcohol", "late_meal", "stress"] as const;

function tagLabel(tag: string): string {
  return tag.replace(/_/g, " ");
}

interface Props {
  annotations: Annotation[];
  onSaved: () => void; // parent re-fetches the list
}

/** One-tap context tagging — the thing commercial apps never let you do. */
export function AnnotationBar({ annotations, onSaved }: Props) {
  const [note, setNote] = useState("");
  const [saving, setSaving] = useState<string | null>(null);
  const [saved, setSaved] = useState<string | null>(null);

  async function tagToday(tag: string) {
    const day = new Date().toISOString().slice(0, 10);
    setSaving(tag);
    await postAnnotation({ day, tag, note: note.trim() });
    setSaving(null);
    setNote("");
    setSaved(tag);
    onSaved();
    setTimeout(() => setSaved(null), 1800);
  }

  const recent = [...annotations]
    .sort((a, b) => (a.day < b.day ? 1 : -1))
    .slice(0, 8);

  return (
    <div className="section">
      <h2>Tag today</h2>
      <div className="tag-row">
        {TAGS.map((t) => (
          <button
            key={t}
            type="button"
            onClick={() => tagToday(t)}
            disabled={saving !== null}
          >
            {saved === t ? "tagged ✓" : saving === t ? "tagging…" : tagLabel(t)}
          </button>
        ))}
        <input
          type="text"
          value={note}
          onChange={(e) => setNote(e.target.value)}
          placeholder="optional note, saved with the next tag"
          aria-label="note for the next tag"
          style={{ flex: "1 1 240px", minWidth: 200 }}
        />
      </div>

      {recent.length > 0 && (
        <ul className="annotation-list">
          {recent.map((a, i) => (
            <li key={`${a.day}-${a.tag}-${i}`}>
              <span className="num ann-day">{a.day}</span>
              <span className="ann-tag">{tagLabel(a.tag)}</span>
              {a.note ? <span className="ann-note">{a.note}</span> : null}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

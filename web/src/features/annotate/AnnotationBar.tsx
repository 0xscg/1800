import { useRef, useState } from "react";
import type { Annotation } from "../../api/types";
import { postAnnotation } from "../../api/client";
import { localDay } from "../../api/mock";

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
  const [error, setError] = useState<string | null>(null);
  const feedbackTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  async function tagToday(tag: string) {
    // A newer confirmation must not be wiped by an older timer.
    if (feedbackTimer.current !== null) clearTimeout(feedbackTimer.current);
    const day = localDay(); // metrics are keyed by LOCAL calendar day
    setSaving(tag);
    setError(null);
    setSaved(null);
    try {
      await postAnnotation({ day, tag, note: note.trim() });
      setNote("");
      setSaved(tag);
      onSaved();
      feedbackTimer.current = setTimeout(() => setSaved(null), 1800);
    } catch {
      setError("Couldn’t save the tag. Try again.");
      feedbackTimer.current = setTimeout(() => setError(null), 4000);
    } finally {
      setSaving(null);
    }
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

      {error ? (
        <div className="dev warn" role="alert" style={{ marginTop: 8 }}>
          {error}
        </div>
      ) : null}

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

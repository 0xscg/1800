import type { MetricToday } from "../../api/types";
import { METRICS } from "../../api/types";

/** z-score → semantic class, respecting metric direction. |z|<0.75 reads as "on baseline". */
export function zClass(z: number | null, higherIsBetter: boolean | null): "good" | "warn" | "flat" {
  if (z == null || Math.abs(z) < 0.75) return "flat";
  if (higherIsBetter == null) return "warn";
  return z > 0 === higherIsBetter ? "good" : "warn";
}

function DeviationDots({ spark, dir }: { spark: number[]; dir: boolean | null }) {
  const cells = [...Array(14)].map((_, i) => spark[spark.length - 14 + i]);
  return (
    <div className="dots" aria-label="last 14 days vs baseline">
      {cells.map((z, i) => (
        <i key={i} className={z === undefined ? "" : zClass(z, dir)} />
      ))}
    </div>
  );
}

interface Props {
  metrics: MetricToday[];
  loading?: boolean;
  selected?: string | null;
  onSelect?: (metric: string) => void;
}

export function TodayStrip({ metrics, loading, selected, onSelect }: Props) {
  if (loading) {
    return <div className="state-note">Reading today’s metrics…</div>;
  }
  if (metrics.length === 0) {
    return (
      <div className="state-note">
        No data yet. Connect Whoop or let the first device sync run.
      </div>
    );
  }
  return (
    <div className="grid">
      {metrics.map((m) => {
        const meta = METRICS[m.metric] ?? {
          label: m.metric, unit: "", higherIsBetter: null, decimals: 0,
        };
        const cls = zClass(m.z, meta.higherIsBetter);
        const delta = m.mean30 != null ? m.value - m.mean30 : null;
        return (
          <button
            type="button"
            className={`card metric-card${selected === m.metric ? " selected" : ""}`}
            key={m.metric}
            onClick={() => onSelect?.(m.metric)}
            aria-pressed={selected === m.metric}
          >
            <div className="label">{meta.label}</div>
            <div>
              <span className="value num">
                {m.value.toFixed(meta.decimals)}
              </span>
              <span className="unit">{meta.unit}</span>
            </div>
            <div className={`dev ${cls}`}>
              {delta == null || m.z == null
                ? "building baseline…"
                : `${delta >= 0 ? "+" : ""}${delta.toFixed(meta.decimals)} vs your 30-day norm (${m.z >= 0 ? "+" : ""}${m.z.toFixed(1)}σ)`}
            </div>
            <DeviationDots spark={m.spark ?? []} dir={meta.higherIsBetter} />
          </button>
        );
      })}
    </div>
  );
}

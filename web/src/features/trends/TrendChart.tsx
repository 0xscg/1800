import { useEffect, useMemo, useState } from "react";
import {
  ComposedChart, Area, Line, XAxis, YAxis, Tooltip, ReferenceLine, ResponsiveContainer,
} from "recharts";
import { fetchSeries } from "../../api/client";
import type { Annotation, SeriesPoint } from "../../api/types";
import { METRICS } from "../../api/types";
import { token } from "../../design/palette";

interface Row extends SeriesPoint {
  // Always numeric: recharts' stack-group domain coerces nulls to 0 and would
  // drag the Y-domain to zero. Null-baseline rows get a zero-height band at value.
  bandLow: number;
  bandHigh: number;
}

const DAY_CHOICES = [30, 90, 180, 365] as const;

interface Props {
  metric: string;
  annotations?: Annotation[];
  /** Show the 30/90/180/365 range selector (focused view). */
  withControls?: boolean;
  days?: number;
  height?: number;
}

/** The signature chart: value line drawn over YOUR ±1σ baseline band. */
export function TrendChart({
  metric,
  annotations = [],
  withControls = false,
  days: fixedDays = 90,
  height = 220,
}: Props) {
  const [days, setDays] = useState(fixedDays);
  const [rows, setRows] = useState<Row[] | null>(null);
  const [failed, setFailed] = useState(false);
  const meta = METRICS[metric] ?? { label: metric, unit: "", higherIsBetter: null, decimals: 0 };

  useEffect(() => setDays(fixedDays), [fixedDays, metric]);

  useEffect(() => {
    let live = true;
    setRows(null);
    setFailed(false);
    fetchSeries(metric, days)
      .then((pts) => {
        if (!live) return;
        setRows(
          pts.map((p) => ({
            ...p,
            bandLow: p.mean30 != null && p.sd30 != null ? p.mean30 - p.sd30 : p.value,
            bandHigh: p.mean30 != null && p.sd30 != null ? p.mean30 + p.sd30 : p.value,
          }))
        );
      })
      .catch(() => live && setFailed(true));
    return () => { live = false; };
  }, [metric, days]);

  // Annotations only make sense as markers on days inside the plotted range.
  const marks = useMemo(() => {
    if (!rows || rows.length === 0) return [];
    const daysInRange = new Set(rows.map((r) => r.day));
    return annotations.filter((a) => daysInRange.has(a.day));
  }, [rows, annotations]);

  const faint = token("text-faint"); // line strokes only — too low-contrast for text
  const dim = token("text-dim"); // informational text (≥4.5:1 on panel)

  return (
    <div className="card trend-card">
      <div className="trend-head">
        <h3>{meta.label}{meta.unit ? <span className="range"> · {meta.unit}</span> : null}</h3>
        {withControls ? (
          <div className="range-picker" role="group" aria-label="range in days">
            {DAY_CHOICES.map((d) => (
              <button
                key={d}
                type="button"
                className={d === days ? "active" : ""}
                onClick={() => setDays(d)}
                aria-pressed={d === days}
              >
                {d}d
              </button>
            ))}
          </div>
        ) : (
          <span className="range">{days}d · band = your 30-day mean ±1σ</span>
        )}
      </div>
      {withControls && (
        <div className="range" style={{ marginBottom: 8 }}>
          band = your 30-day mean ±1σ{marks.length > 0 ? " · tagged days marked" : ""}
        </div>
      )}
      {failed ? (
        <div className="state-note">Couldn’t load this series. Retry by changing the range.</div>
      ) : rows === null ? (
        <div className="state-note" style={{ height }}>Loading series…</div>
      ) : rows.length === 0 ? (
        <div className="state-note" style={{ height }}>Building baseline…</div>
      ) : (
        <ResponsiveContainer width="100%" height={height}>
          <ComposedChart data={rows} margin={{ top: 18, right: 6, left: -14, bottom: 0 }}>
            <XAxis
              dataKey="day"
              tick={{ fill: dim, fontSize: 11, fontFamily: "JetBrains Mono" }}
              tickFormatter={(d: string) => d.slice(5)}
              axisLine={false} tickLine={false} minTickGap={40}
            />
            <YAxis
              tick={{ fill: dim, fontSize: 11, fontFamily: "JetBrains Mono" }}
              axisLine={false} tickLine={false} domain={["auto", "auto"]} width={54}
            />
            <Tooltip
              contentStyle={{
                background: token("panel"), border: `1px solid ${token("panel-edge")}`,
                borderRadius: 8, fontFamily: "JetBrains Mono", fontSize: 12,
              }}
              labelStyle={{ color: token("text-dim") }}
              formatter={(v: number, name: string) =>
                name === "baseline band" ? [] : [Number(v).toFixed(meta.decimals), name]
              }
            />
            {/* baseline band: two stacked areas so only the σ-width is filled */}
            <Area dataKey="bandLow" stackId="band" stroke="none" fill="transparent" isAnimationActive={false} legendType="none" tooltipType="none" />
            <Area
              dataKey={(r: Row) => r.bandHigh - r.bandLow}
              stackId="band" stroke="none" fill={token("band")} fillOpacity={0.9} isAnimationActive={false}
              name="baseline band"
            />
            {/* context tags as quiet day markers */}
            {marks.map((a, i) => (
              <ReferenceLine
                key={`${a.day}-${a.tag}-${i}`}
                x={a.day}
                stroke={faint}
                strokeDasharray="2 3"
                label={{
                  value: a.tag, position: "top", fill: dim,
                  fontSize: 10, fontFamily: "JetBrains Mono",
                }}
              />
            ))}
            <Line dataKey="mean30" name="30d mean" stroke={faint} strokeDasharray="4 4" dot={false} strokeWidth={1} isAnimationActive={false} />
            <Line dataKey="value" name={meta.label} stroke={token("sage")} dot={false} strokeWidth={2} isAnimationActive={false} />
          </ComposedChart>
        </ResponsiveContainer>
      )}
    </div>
  );
}

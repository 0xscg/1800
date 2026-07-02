import { useEffect, useState } from "react";
import {
  ComposedChart, Area, Line, XAxis, YAxis, Tooltip, ResponsiveContainer,
} from "recharts";
import { fetchSeries } from "../../api/client";
import type { SeriesPoint } from "../../api/types";
import { METRICS } from "../../api/types";

interface Row extends SeriesPoint {
  bandLow: number | null;
  bandHigh: number | null;
}

/** The signature chart: value line drawn over YOUR ±1σ baseline band. */
export function TrendChart({ metric, days = 90 }: { metric: string; days?: number }) {
  const [rows, setRows] = useState<Row[]>([]);
  const meta = METRICS[metric] ?? { label: metric, unit: "", higherIsBetter: null, decimals: 0 };

  useEffect(() => {
    fetchSeries(metric, days).then((pts) =>
      setRows(
        pts.map((p) => ({
          ...p,
          bandLow: p.mean30 != null && p.sd30 != null ? p.mean30 - p.sd30 : null,
          bandHigh: p.mean30 != null && p.sd30 != null ? p.mean30 + p.sd30 : null,
        }))
      )
    );
  }, [metric, days]);

  return (
    <div className="card trend-card">
      <div className="trend-head">
        <h3>{meta.label}</h3>
        <span className="range">{days}d · band = your 30-day mean ±1σ</span>
      </div>
      <ResponsiveContainer width="100%" height={220}>
        <ComposedChart data={rows} margin={{ top: 6, right: 6, left: -14, bottom: 0 }}>
          <XAxis
            dataKey="day"
            tick={{ fill: "#566270", fontSize: 11, fontFamily: "JetBrains Mono" }}
            tickFormatter={(d: string) => d.slice(5)}
            axisLine={false} tickLine={false} minTickGap={40}
          />
          <YAxis
            tick={{ fill: "#566270", fontSize: 11, fontFamily: "JetBrains Mono" }}
            axisLine={false} tickLine={false} domain={["auto", "auto"]} width={54}
          />
          <Tooltip
            contentStyle={{
              background: "#131a21", border: "1px solid #1e2731",
              borderRadius: 8, fontFamily: "JetBrains Mono", fontSize: 12,
            }}
            labelStyle={{ color: "#8b98a5" }}
          />
          {/* baseline band: two stacked areas so only the σ-width is filled */}
          <Area dataKey="bandLow" stackId="band" stroke="none" fill="transparent" isAnimationActive={false} />
          <Area
            dataKey={(r: Row) =>
              r.bandHigh != null && r.bandLow != null ? r.bandHigh - r.bandLow : null
            }
            stackId="band" stroke="none" fill="#22303b" fillOpacity={0.9} isAnimationActive={false}
            name="baseline band"
          />
          <Line dataKey="mean30" stroke="#566270" strokeDasharray="4 4" dot={false} strokeWidth={1} isAnimationActive={false} />
          <Line dataKey="value" stroke="#8fbca8" dot={false} strokeWidth={2} isAnimationActive={false} />
        </ComposedChart>
      </ResponsiveContainer>
    </div>
  );
}

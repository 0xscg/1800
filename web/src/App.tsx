import { useEffect, useState } from "react";
import { fetchToday } from "./api/client";
import type { MetricToday } from "./api/types";
import { TodayStrip } from "./features/today/TodayStrip";
import { TrendChart } from "./features/trends/TrendChart";
import { AnnotationBar } from "./features/annotate/AnnotationBar";

const HERO_TRENDS = ["hrv_rmssd_ms", "resting_hr", "sleep_min", "recovery_score"];

export default function App() {
  const [today, setToday] = useState<MetricToday[]>([]);

  useEffect(() => {
    fetchToday().then(setToday);
  }, []);

  const dateLabel = new Date().toLocaleDateString("en-GB", {
    weekday: "short", day: "numeric", month: "short",
  });

  return (
    <div className="page">
      <header className="masthead">
        <h1>Baseline</h1>
        <span className="date">{dateLabel} · you vs. you</span>
      </header>

      <TodayStrip metrics={today} />

      <section className="section">
        <h2>Trends</h2>
        <div className="grid" style={{ gridTemplateColumns: "repeat(auto-fill, minmax(440px, 1fr))" }}>
          {HERO_TRENDS.map((m) => (
            <TrendChart key={m} metric={m} />
          ))}
        </div>
      </section>

      <AnnotationBar />
    </div>
  );
}

import { useCallback, useEffect, useState } from "react";
import { fetchAnnotations, fetchToday } from "./api/client";
import type { Annotation, MetricToday } from "./api/types";
import { METRICS } from "./api/types";
import { TodayStrip } from "./features/today/TodayStrip";
import { TrendChart } from "./features/trends/TrendChart";
import { AnnotationBar } from "./features/annotate/AnnotationBar";

const HERO_TRENDS = ["hrv_rmssd_ms", "resting_hr", "sleep_min", "recovery_score"];

export default function App() {
  const [today, setToday] = useState<MetricToday[]>([]);
  const [loadingToday, setLoadingToday] = useState(true);
  const [annotations, setAnnotations] = useState<Annotation[]>([]);
  const [selected, setSelected] = useState<string>("hrv_rmssd_ms");

  useEffect(() => {
    fetchToday()
      .then(setToday)
      .finally(() => setLoadingToday(false));
  }, []);

  const refreshAnnotations = useCallback(() => {
    fetchAnnotations().then(setAnnotations);
  }, []);

  useEffect(refreshAnnotations, [refreshAnnotations]);

  const dateLabel = new Date().toLocaleDateString("en-GB", {
    weekday: "short", day: "numeric", month: "short",
  });

  const selectedLabel = METRICS[selected]?.label ?? selected;
  const heroRest = HERO_TRENDS.filter((m) => m !== selected);

  return (
    <div className="page">
      <header className="masthead">
        <h1>Baseline</h1>
        <span className="date">{dateLabel} · you vs. you</span>
      </header>

      <TodayStrip
        metrics={today}
        loading={loadingToday}
        selected={selected}
        onSelect={setSelected}
      />

      <section className="section">
        <h2>{selectedLabel} — focus</h2>
        <TrendChart
          key={selected}
          metric={selected}
          annotations={annotations}
          withControls
          days={90}
          height={280}
        />
      </section>

      <section className="section">
        <h2>Trends</h2>
        <div className="grid" style={{ gridTemplateColumns: "repeat(auto-fill, minmax(440px, 1fr))" }}>
          {heroRest.map((m) => (
            <TrendChart key={m} metric={m} annotations={annotations} />
          ))}
        </div>
      </section>

      <AnnotationBar annotations={annotations} onSaved={refreshAnnotations} />
    </div>
  );
}

import type { MetricToday, SeriesPoint } from "./types";

// Deterministic pseudo-random so the mock dashboard is stable between reloads.
function rng(seed: number) {
  return () => {
    seed = (seed * 9301 + 49297) % 233280;
    return seed / 233280;
  };
}

const BASES: Record<string, { base: number; noise: number }> = {
  hrv_rmssd_ms:     { base: 62,   noise: 10 },
  resting_hr:       { base: 52,   noise: 3 },
  sleep_min:        { base: 430,  noise: 45 },
  sleep_efficiency: { base: 91,   noise: 4 },
  recovery_score:   { base: 68,   noise: 15 },
  respiratory_rate: { base: 14.8, noise: 0.5 },
  steps:            { base: 9200, noise: 2600 },
  vo2max:           { base: 46.5, noise: 0.4 },
};

export function mockSeries(metric: string, days: number): SeriesPoint[] {
  const cfg = BASES[metric] ?? { base: 50, noise: 8 };
  const rand = rng(metric.length * 7919);
  const out: SeriesPoint[] = [];
  const today = new Date();
  let drift = 0;
  for (let i = days; i >= 0; i--) {
    drift += (rand() - 0.5) * cfg.noise * 0.15;
    const value = cfg.base + drift + (rand() - 0.5) * cfg.noise;
    const d = new Date(today);
    d.setDate(d.getDate() - i);
    out.push({
      day: d.toISOString().slice(0, 10),
      value: Math.round(value * 10) / 10,
      mean30: Math.round((cfg.base + drift * 0.6) * 10) / 10,
      sd30: Math.round(cfg.noise * 0.55 * 10) / 10,
    });
  }
  return out;
}

export function mockToday(): MetricToday[] {
  return Object.keys(BASES).map((metric) => {
    const s = mockSeries(metric, 20);
    const last = s[s.length - 1];
    const z =
      last.mean30 != null && last.sd30 ? (last.value - last.mean30) / last.sd30 : null;
    const spark = s.slice(-14).map((p) =>
      p.mean30 != null && p.sd30 ? (p.value - p.mean30) / p.sd30 : 0
    );
    return { metric, day: last.day, value: last.value, mean30: last.mean30, sd30: last.sd30, z, spark };
  });
}

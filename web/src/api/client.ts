import type { MetricToday, SeriesPoint } from "./types";
import { mockToday, mockSeries } from "./mock";

// Falls back to mock data when the backend isn't running,
// so the dashboard is designable before any device is connected.
async function get<T>(path: string, fallback: T): Promise<T> {
  try {
    const res = await fetch(path);
    if (!res.ok) throw new Error(String(res.status));
    return (await res.json()) as T;
  } catch {
    return fallback;
  }
}

export const fetchToday = () => get<MetricToday[]>("/v1/dashboard/today", mockToday());

export const fetchSeries = (metric: string, days = 90) =>
  get<SeriesPoint[]>(`/v1/metrics/${metric}?days=${days}`, mockSeries(metric, days));

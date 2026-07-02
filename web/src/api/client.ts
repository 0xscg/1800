import type { Annotation, MetricToday, SeriesPoint } from "./types";
import { mockToday, mockSeries, mockAnnotations, mockAddAnnotation } from "./mock";

// Falls back to mock data when the backend isn't running,
// so the dashboard is designable before any device is connected.
async function get<T>(path: string, fallback: () => T): Promise<T> {
  try {
    const res = await fetch(path);
    if (!res.ok) throw new Error(String(res.status));
    return (await res.json()) as T;
  } catch {
    return fallback();
  }
}

export const fetchToday = () => get<MetricToday[]>("/v1/dashboard/today", mockToday);

export const fetchSeries = (metric: string, days = 90) =>
  get<SeriesPoint[]>(`/v1/metrics/${metric}?days=${days}`, () => mockSeries(metric, days));

export const fetchAnnotations = () =>
  get<Annotation[]>("/v1/annotations", mockAnnotations);

/** POST an annotation; in mock mode it lands in the in-memory store instead. */
export async function postAnnotation(a: Annotation): Promise<void> {
  try {
    const res = await fetch("/v1/annotations", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(a),
    });
    if (!res.ok) throw new Error(String(res.status));
  } catch {
    mockAddAnnotation(a);
  }
}

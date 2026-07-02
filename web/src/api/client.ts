import type { Annotation, MetricToday, SeriesPoint } from "./types";
import { mockToday, mockSeries, mockAnnotations, mockAddAnnotation } from "./mock";

// Falls back to mock data when the backend isn't running,
// so the dashboard is designable before any device is connected.
//
// Mode is latched per session: the first request that reaches (or fails to reach)
// the backend decides mock-vs-live for ALL subsequent calls, including POSTs,
// so cards, charts and annotations never mix datasets mid-session.
let mode: "live" | "mock" | null = null;

async function get<T>(path: string, fallback: () => T): Promise<T> {
  if (mode === "mock") return fallback();
  try {
    const res = await fetch(path);
    if (!res.ok) throw new Error(String(res.status));
    const body = (await res.json()) as T;
    mode = "live"; // the backend answered properly — stay live for the whole session
    return body;
  } catch (err) {
    if (mode === "live") throw err; // live backend errored — surface it, don't mix in mock
    // First contact failed (network error, or the dev proxy 5xx-ing with no backend)
    // — latch mock for the whole session.
    mode = "mock";
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
  if (mode === "mock") {
    mockAddAnnotation(a);
    return;
  }
  let res: Response;
  try {
    res = await fetch("/v1/annotations", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(a),
    });
  } catch (err) {
    // Only a network-level failure means "no backend". If we're already live, surface it.
    if (mode === "live") throw err;
    mode = "mock";
    mockAddAnnotation(a);
    return;
  }
  mode = "live";
  if (!res.ok) throw new Error(String(res.status));
}

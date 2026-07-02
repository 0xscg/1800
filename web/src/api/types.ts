export interface MetricToday {
  metric: string;
  day: string;
  value: number;
  mean30: number | null;
  sd30: number | null;
  z: number | null;
  spark: number[]; // last 14 z-scores, oldest first
}

export interface SeriesPoint {
  day: string;
  value: number;
  mean30: number | null;
  sd30: number | null;
}

export interface Annotation {
  day: string; // date, required
  tag: string; // required
  note?: string;
}

/** Metrics the device shim may POST — mirrors DeviceSample.metric enum in contracts/openapi.yaml. */
export type DeviceSampleMetric =
  | "steps"
  | "active_kcal"
  | "vo2max"
  | "hrv_sdnn_ms"
  | "hrv_rmssd_ms"
  | "resting_hr"
  | "sleep_min";

export interface DeviceSample {
  day: string;
  metric: DeviceSampleMetric;
  value: number;
}

/** Display metadata. `higherIsBetter: null` = deviation in either direction is just "off baseline". */
export const METRICS: Record<
  string,
  { label: string; unit: string; higherIsBetter: boolean | null; decimals: number }
> = {
  hrv_rmssd_ms:     { label: "HRV (rMSSD)",     unit: "ms",     higherIsBetter: true,  decimals: 0 },
  resting_hr:       { label: "Resting HR",       unit: "bpm",    higherIsBetter: false, decimals: 0 },
  sleep_min:        { label: "Sleep",            unit: "min",    higherIsBetter: true,  decimals: 0 },
  sleep_efficiency: { label: "Sleep efficiency", unit: "%",      higherIsBetter: true,  decimals: 0 },
  recovery_score:   { label: "Recovery",         unit: "/100",   higherIsBetter: true,  decimals: 0 },
  respiratory_rate: { label: "Respiratory rate", unit: "br/min", higherIsBetter: null,  decimals: 1 },
  steps:            { label: "Steps",            unit: "",       higherIsBetter: true,  decimals: 0 },
  active_kcal:      { label: "Active energy",    unit: "kcal",   higherIsBetter: true,  decimals: 0 },
  vo2max:           { label: "VO₂max",           unit: "",       higherIsBetter: true,  decimals: 1 },
  hrv_sdnn_ms:      { label: "HRV (SDNN, watch)",unit: "ms",     higherIsBetter: true,  decimals: 0 },
};

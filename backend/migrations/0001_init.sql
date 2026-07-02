-- 0001_init.sql — schema is derivation-friendly: raw is immutable truth, everything else rebuilds.

CREATE TABLE oauth_tokens (
  provider      text PRIMARY KEY,            -- 'whoop'
  access_token  text NOT NULL,
  refresh_token text NOT NULL,
  expires_at    timestamptz NOT NULL,
  updated_at    timestamptz NOT NULL DEFAULT now()
);

-- Immutable-ish event log. Whoop resends updates for the same UUID; we upsert payload.
CREATE TABLE raw_events (
  id          bigserial PRIMARY KEY,
  provider    text NOT NULL,                 -- 'whoop' | 'healthkit' | 'health_connect'
  kind        text NOT NULL,                 -- 'sleep' | 'recovery' | 'workout' | 'sample_batch'
  external_id text NOT NULL,                 -- whoop UUID or device batch id
  payload     jsonb NOT NULL,
  received_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (provider, kind, external_id)
);

-- Normalized daily metrics. (day, metric, source) unique => idempotent re-derivation.
CREATE TABLE daily_metrics (
  day    date  NOT NULL,
  metric text  NOT NULL,                     -- 'hrv_rmssd_ms','resting_hr','sleep_min','sleep_efficiency','recovery_score','respiratory_rate','steps','active_kcal','vo2max','hrv_sdnn_ms'
  source text  NOT NULL,                     -- 'whoop' | 'watch'
  value  double precision NOT NULL,
  UNIQUE (day, metric, source)
);
CREATE INDEX idx_daily_metrics_metric_day ON daily_metrics (metric, day);

CREATE TABLE annotations (
  id   bigserial PRIMARY KEY,
  day  date NOT NULL,
  tag  text NOT NULL,                        -- 'travel','illness','alcohol','late_meal','stress',...
  note text
);
CREATE INDEX idx_annotations_day ON annotations (day);

-- Source-of-truth policy per metric: whoop wins sleep/recovery physiology, watch wins activity.
CREATE VIEW metric_preferred AS
SELECT DISTINCT ON (day, metric) day, metric, source, value
FROM daily_metrics
ORDER BY day, metric,
  CASE
    WHEN metric IN ('hrv_rmssd_ms','resting_hr','sleep_min','sleep_efficiency','recovery_score','respiratory_rate') AND source = 'whoop' THEN 0
    WHEN metric IN ('steps','active_kcal','vo2max','hrv_sdnn_ms')                                                   AND source = 'watch' THEN 0
    ELSE 1
  END;

-- The product: every day scored against YOUR trailing 30-day baseline (excluding today).
CREATE VIEW metric_scored AS
SELECT
  day, metric, value,
  AVG(value)        OVER w AS mean30,
  STDDEV_SAMP(value) OVER w AS sd30,
  (value - AVG(value) OVER w) / NULLIF(STDDEV_SAMP(value) OVER w, 0) AS z
FROM metric_preferred
WINDOW w AS (PARTITION BY metric ORDER BY day ROWS BETWEEN 30 PRECEDING AND 1 PRECEDING);

package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

// MetricToday: latest value per metric + z against personal 30d baseline + 14-day z sparkline.
type MetricToday struct {
	Metric string    `json:"metric"`
	Day    string    `json:"day"`
	Value  float64   `json:"value"`
	Mean30 *float64  `json:"mean30"`
	SD30   *float64  `json:"sd30"`
	Z      *float64  `json:"z"`
	Spark  []float64 `json:"spark"` // last 14 z-scores, oldest first (0 where undefined)
}

func (a *API) today(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := a.Store.Pool.Query(ctx, `
		SELECT DISTINCT ON (metric) metric, day, value, mean30, sd30, z
		FROM metric_scored
		ORDER BY metric, day DESC`)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	var out []MetricToday
	for rows.Next() {
		var m MetricToday
		var day time.Time
		if err := rows.Scan(&m.Metric, &day, &m.Value, &m.Mean30, &m.SD30, &m.Z); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		m.Day = day.Format("2006-01-02")
		out = append(out, m)
	}

	// Sparklines: one query for all metrics' last 14 z-scores.
	spark := map[string][]float64{}
	srows, err := a.Store.Pool.Query(ctx, `
		SELECT metric, z FROM (
			SELECT metric, day, z, ROW_NUMBER() OVER (PARTITION BY metric ORDER BY day DESC) rn
			FROM metric_scored
		) t WHERE rn <= 14 ORDER BY metric, day ASC`)
	if err == nil {
		defer srows.Close()
		for srows.Next() {
			var metric string
			var z *float64
			if srows.Scan(&metric, &z) == nil {
				v := 0.0
				if z != nil {
					v = *z
				}
				spark[metric] = append(spark[metric], v)
			}
		}
	}
	for i := range out {
		out[i].Spark = spark[out[i].Metric]
	}
	writeJSON(w, out)
}

type SeriesPoint struct {
	Day    string   `json:"day"`
	Value  float64  `json:"value"`
	Mean30 *float64 `json:"mean30"`
	SD30   *float64 `json:"sd30"`
}

func (a *API) series(w http.ResponseWriter, r *http.Request) {
	metric := chi.URLParam(r, "metric")
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days <= 0 || days > 730 {
		days = 90
	}
	rows, err := a.Store.Pool.Query(r.Context(), `
		SELECT day, value, mean30, sd30 FROM metric_scored
		WHERE metric = $1 AND day >= (CURRENT_DATE - $2::int)
		ORDER BY day ASC`, metric, days)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()
	out := []SeriesPoint{}
	for rows.Next() {
		var p SeriesPoint
		var day time.Time
		if err := rows.Scan(&day, &p.Value, &p.Mean30, &p.SD30); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		p.Day = day.Format("2006-01-02")
		out = append(out, p)
	}
	writeJSON(w, out)
}

type Annotation struct {
	ID   int64  `json:"id"`
	Day  string `json:"day"`
	Tag  string `json:"tag"`
	Note string `json:"note"`
}

func (a *API) listAnnotations(w http.ResponseWriter, r *http.Request) {
	rows, err := a.Store.Pool.Query(r.Context(),
		`SELECT id, day, tag, COALESCE(note,'') FROM annotations ORDER BY day DESC LIMIT 200`)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()
	out := []Annotation{}
	for rows.Next() {
		var an Annotation
		var day time.Time
		if rows.Scan(&an.ID, &day, &an.Tag, &an.Note) == nil {
			an.Day = day.Format("2006-01-02")
			out = append(out, an)
		}
	}
	writeJSON(w, out)
}

func (a *API) createAnnotation(w http.ResponseWriter, r *http.Request) {
	var in Annotation
	if err := readJSON(r, &in); err != nil {
		http.Error(w, "bad body", 400)
		return
	}
	day, err := time.Parse("2006-01-02", in.Day)
	if err != nil {
		http.Error(w, "day must be YYYY-MM-DD", 400)
		return
	}
	err = a.Store.Pool.QueryRow(r.Context(),
		`INSERT INTO annotations (day, tag, note) VALUES ($1,$2,$3) RETURNING id`,
		day, in.Tag, in.Note).Scan(&in.ID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, in)
}

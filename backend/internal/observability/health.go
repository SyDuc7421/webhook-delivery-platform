package observability

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// readyTimeout bounds every readiness check (docs/OBSERVABILITY.md §Health).
const readyTimeout = 2 * time.Second

// Check is one dependency probe for /readyz. A failing non-critical check is reported but
// does not make the process unready — Redis is non-critical by design (NFR-D1).
type Check struct {
	Name     string
	Critical bool
	Probe    func(ctx context.Context) error
}

// Health serves liveness and readiness.
type Health struct {
	checks []Check
}

// NewHealth returns a Health running the given checks on /readyz.
func NewHealth(checks ...Check) *Health {
	return &Health{checks: checks}
}

type checkResult struct {
	Status   string `json:"status"`
	Critical bool   `json:"critical"`
	Error    string `json:"error,omitempty"`
}

type readyResponse struct {
	Status string                 `json:"status"`
	Checks map[string]checkResult `json:"checks"`
}

// Live is the /healthz handler: always 200 while the process can serve HTTP.
func (h *Health) Live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Ready is the /readyz handler: runs every check concurrently with a 2 s timeout.
func (h *Health) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
	defer cancel()

	results := make(map[string]checkResult, len(h.checks))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, c := range h.checks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := checkResult{Status: "up", Critical: c.Critical}
			if err := c.Probe(ctx); err != nil {
				res.Status = "down"
				res.Error = err.Error()
			}
			mu.Lock()
			results[c.Name] = res
			mu.Unlock()
		}()
	}
	wg.Wait()

	status, code := "ready", http.StatusOK
	for _, res := range results {
		if res.Critical && res.Status != "up" {
			status, code = "not_ready", http.StatusServiceUnavailable
			break
		}
	}
	writeJSON(w, code, readyResponse{Status: status, Checks: results})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v) // write error means the client went away; nothing to do
}

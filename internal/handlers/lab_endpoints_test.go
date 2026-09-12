package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"platarium-gateway-go/internal/nodes"
	"platarium-gateway-go/internal/rating"

	"github.com/gorilla/mux"
)

func TestLabEndpointsEnabledDefaultFailClosed(t *testing.T) {
	t.Setenv("ENABLE_LAB_ENDPOINTS", "")
	if LabEndpointsEnabled() {
		t.Fatal("default must be fail-closed")
	}
	t.Setenv("ENABLE_LAB_ENDPOINTS", "0")
	if LabEndpointsEnabled() {
		t.Fatal("0 must be disabled")
	}
	t.Setenv("ENABLE_LAB_ENDPOINTS", "true")
	if !LabEndpointsEnabled() {
		t.Fatal("true must enable")
	}
}

func TestLabRoutesAbsentByDefault(t *testing.T) {
	_ = os.Unsetenv("ENABLE_LAB_ENDPOINTS")
	r := mux.NewRouter()
	RegisterLabRoutes(r, &Handler{})
	for _, path := range []string{"/api/test-set-load", "/api/reward-credit-l1"} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("%s: want 404 got %d", path, rr.Code)
		}
	}
}

func TestLabRoutesRegisteredWhenEnabled(t *testing.T) {
	t.Setenv("ENABLE_LAB_ENDPOINTS", "1")
	r := mux.NewRouter()
	RegisterLabRoutes(r, &Handler{})
	found := map[string]bool{}
	_ = r.Walk(func(route *mux.Route, _ *mux.Router, _ []*mux.Route) error {
		path, err := route.GetPathTemplate()
		if err != nil {
			return nil
		}
		found[path] = true
		return nil
	})
	if !found["/api/test-set-load"] || !found["/api/reward-credit-l1"] {
		t.Fatalf("lab routes missing when enabled: %v", found)
	}
}

func TestLabAuthMatrixTestSetLoad(t *testing.T) {
	path := "/api/test-set-load"
	t.Run("lab_off_404", func(t *testing.T) {
		t.Setenv("ENABLE_LAB_ENDPOINTS", "")
		r := mux.NewRouter()
		RegisterLabRoutes(r, labTestHandler())
		rr := postLab(r, path, nil, "")
		if rr.Code != http.StatusNotFound {
			t.Fatalf("want 404 got %d", rr.Code)
		}
	})
	t.Run("lab_on_no_token_401", func(t *testing.T) {
		t.Setenv("ENABLE_LAB_ENDPOINTS", "1")
		t.Setenv("PLATARIUM_LAB_TOKEN", "lab-secret")
		r := mux.NewRouter()
		RegisterLabRoutes(r, labTestHandler())
		rr := postLab(r, path, map[string]interface{}{"currentTasks": 1, "maxCapacity": 10}, "")
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("want 401 got %d", rr.Code)
		}
	})
	t.Run("lab_on_empty_env_token_401", func(t *testing.T) {
		t.Setenv("ENABLE_LAB_ENDPOINTS", "1")
		t.Setenv("PLATARIUM_LAB_TOKEN", "")
		r := mux.NewRouter()
		RegisterLabRoutes(r, labTestHandler())
		rr := postLab(r, path, map[string]interface{}{"currentTasks": 1, "maxCapacity": 10}, "anything")
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("want 401 got %d", rr.Code)
		}
	})
	t.Run("lab_on_invalid_token_403", func(t *testing.T) {
		t.Setenv("ENABLE_LAB_ENDPOINTS", "1")
		t.Setenv("PLATARIUM_LAB_TOKEN", "lab-secret")
		r := mux.NewRouter()
		RegisterLabRoutes(r, labTestHandler())
		rr := postLab(r, path, map[string]interface{}{"currentTasks": 1, "maxCapacity": 10}, "wrong")
		if rr.Code != http.StatusForbidden {
			t.Fatalf("want 403 got %d", rr.Code)
		}
	})
	t.Run("lab_on_valid_token_ok", func(t *testing.T) {
		t.Setenv("ENABLE_LAB_ENDPOINTS", "1")
		t.Setenv("PLATARIUM_LAB_TOKEN", "lab-secret")
		r := mux.NewRouter()
		RegisterLabRoutes(r, labTestHandler())
		rr := postLab(r, path, map[string]interface{}{"currentTasks": 2, "maxCapacity": 10}, "lab-secret")
		if rr.Code != http.StatusOK {
			t.Fatalf("want 200 got %d body=%s", rr.Code, rr.Body.String())
		}
	})
}

func TestLabAuthMatrixRewardCreditL1(t *testing.T) {
	path := "/api/reward-credit-l1"
	t.Run("lab_off_404", func(t *testing.T) {
		t.Setenv("ENABLE_LAB_ENDPOINTS", "0")
		h := labTestHandler()
		h.nodeEarnedL1 = 42
		r := mux.NewRouter()
		RegisterLabRoutes(r, h)
		rr := postLab(r, path, map[string]interface{}{"amount": 7}, "")
		if rr.Code != http.StatusNotFound {
			t.Fatalf("want 404 got %d", rr.Code)
		}
		if h.nodeEarnedL1 != 42 {
			t.Fatalf("nodeEarnedL1 changed without route: %d", h.nodeEarnedL1)
		}
	})
	t.Run("lab_on_no_token_unchanged", func(t *testing.T) {
		t.Setenv("ENABLE_LAB_ENDPOINTS", "1")
		t.Setenv("PLATARIUM_LAB_TOKEN", "lab-secret")
		h := labTestHandler()
		h.nodeEarnedL1 = 100
		r := mux.NewRouter()
		RegisterLabRoutes(r, h)
		rr := postLab(r, path, map[string]interface{}{"amount": 5}, "")
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("want 401 got %d", rr.Code)
		}
		if h.nodeEarnedL1 != 100 {
			t.Fatalf("nodeEarnedL1 mutated without auth: %d", h.nodeEarnedL1)
		}
	})
	t.Run("lab_on_invalid_token_unchanged", func(t *testing.T) {
		t.Setenv("ENABLE_LAB_ENDPOINTS", "1")
		t.Setenv("PLATARIUM_LAB_TOKEN", "lab-secret")
		h := labTestHandler()
		h.nodeEarnedL1 = 100
		r := mux.NewRouter()
		RegisterLabRoutes(r, h)
		rr := postLab(r, path, map[string]interface{}{"amount": 5}, "nope")
		if rr.Code != http.StatusForbidden {
			t.Fatalf("want 403 got %d", rr.Code)
		}
		if h.nodeEarnedL1 != 100 {
			t.Fatalf("nodeEarnedL1 mutated on 403: %d", h.nodeEarnedL1)
		}
	})
	t.Run("lab_on_valid_token_ok", func(t *testing.T) {
		t.Setenv("ENABLE_LAB_ENDPOINTS", "1")
		t.Setenv("PLATARIUM_LAB_TOKEN", "lab-secret")
		h := labTestHandler()
		h.nodeEarnedL1 = 10
		r := mux.NewRouter()
		RegisterLabRoutes(r, h)
		rr := postLab(r, path, map[string]interface{}{"amount": 5}, "lab-secret")
		if rr.Code != http.StatusOK {
			t.Fatalf("want 200 got %d body=%s", rr.Code, rr.Body.String())
		}
		if h.nodeEarnedL1 != 15 {
			t.Fatalf("nodeEarnedL1=%d want 15", h.nodeEarnedL1)
		}
	})
}

func labTestHandler() *Handler {
	nm := nodes.NewTestNodesManager()
	return &Handler{
		nodesManager: nm,
		nodeRegistry: rating.NewRegistry(),
	}
}

func postLab(r *mux.Router, path string, body map[string]interface{}, token string) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	if token != "" {
		req.Header.Set("X-Platarium-Lab-Token", token)
	}
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	return rr
}

// TestNoUnprotectedLabHandlerAliases audits alternate REST paths (issue #42 / TASK-013).
// Checklist of scanned routes: LabMutationCanonicalPaths + LabMutationAliasCandidates.
// Only canonical paths bind handlers, and only via RegisterLabRoutes + requireLabAuth.
func TestNoUnprotectedLabHandlerAliases(t *testing.T) {
	canonical := map[string]bool{}
	for _, p := range LabMutationCanonicalPaths() {
		canonical[p] = true
	}
	for _, alias := range LabMutationAliasCandidates() {
		if canonical[alias] {
			t.Fatalf("alias %q overlaps canonical lab path", alias)
		}
	}

	t.Run("lab_off_aliases_404", func(t *testing.T) {
		t.Setenv("ENABLE_LAB_ENDPOINTS", "")
		r := mux.NewRouter()
		RegisterLabRoutes(r, labTestHandler())
		for _, path := range append(LabMutationCanonicalPaths(), LabMutationAliasCandidates()...) {
			rr := postLab(r, path, map[string]interface{}{"currentTasks": 1, "maxCapacity": 10, "amount": 1}, "lab-secret")
			if rr.Code != http.StatusNotFound {
				t.Fatalf("%s lab_off: want 404 got %d", path, rr.Code)
			}
		}
	})

	t.Run("lab_on_aliases_still_404", func(t *testing.T) {
		t.Setenv("ENABLE_LAB_ENDPOINTS", "1")
		t.Setenv("PLATARIUM_LAB_TOKEN", "lab-secret")
		h := labTestHandler()
		h.nodeEarnedL1 = 50
		r := mux.NewRouter()
		RegisterLabRoutes(r, h)
		found := map[string]bool{}
		_ = r.Walk(func(route *mux.Route, _ *mux.Router, _ []*mux.Route) error {
			path, err := route.GetPathTemplate()
			if err != nil {
				return nil
			}
			found[path] = true
			return nil
		})
		for _, path := range LabMutationCanonicalPaths() {
			if !found[path] {
				t.Fatalf("canonical lab path missing when enabled: %s", path)
			}
		}
		for _, alias := range LabMutationAliasCandidates() {
			if found[alias] {
				t.Fatalf("unprotected/alias lab route registered: %s", alias)
			}
			rr := postLab(r, alias, map[string]interface{}{"currentTasks": 9, "maxCapacity": 10, "amount": 7}, "lab-secret")
			if rr.Code != http.StatusNotFound {
				t.Fatalf("%s lab_on alias: want 404 got %d", alias, rr.Code)
			}
		}
		if h.nodeEarnedL1 != 50 {
			t.Fatalf("alias must not mutate L1 earnings: %d", h.nodeEarnedL1)
		}
	})
}

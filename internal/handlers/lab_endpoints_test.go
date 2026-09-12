package handlers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

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

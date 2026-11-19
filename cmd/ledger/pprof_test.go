package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"ledger/internal/httpapi"
)

func TestPprofServerServesProfiles(t *testing.T) {
	server := httptest.NewServer(newPprofServer("").Handler)
	defer server.Close()

	response, err := http.Get(server.URL + "/debug/pprof/heap?debug=1")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("heap profile status = %d, want 200", response.StatusCode)
	}
}

// TestAPIDoesNotServeProfiles guards the reason the profiles live on their own listener:
// the public API handler must answer 404 for them.
func TestAPIDoesNotServeProfiles(t *testing.T) {
	api := httpapi.NewHandler(nil, nil)
	for _, path := range []string{"/debug/pprof/", "/debug/pprof/heap"} {
		recorder := httptest.NewRecorder()
		api.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusNotFound {
			t.Errorf("API served %s with status %d, want 404", path, recorder.Code)
		}
	}
}

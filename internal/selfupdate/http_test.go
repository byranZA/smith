package selfupdate

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPGetReturnsTheBodyServed(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte("checksums")); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	got, err := HTTP{Client: srv.Client()}.Get(t.Context(), srv.URL)

	if err != nil {
		t.Fatalf("Get(%s) errored: %v", srv.URL, err)
	}
	if string(got) != "checksums" {
		t.Errorf("Get(%s) = %q, want %q", srv.URL, got, "checksums")
	}
}

func TestHTTPGetTreatsAFailedResponseAsAnErrorNamingTheStatus(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	_, err := HTTP{Client: srv.Client()}.Get(t.Context(), srv.URL)

	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("Get(%s) error = %v, want one naming the 404", srv.URL, err)
	}
}

package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func Test_notFoundPage(t *testing.T) {
	req, err := http.NewRequest("GET", "/doesnotexist", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(notFoundPage)

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusNotFound {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}

	expected := `<h1>404 page not found</h1>`
	if rr.Body.String() != expected {
		t.Errorf("handler returned unexpected body: got %v want %v",
			rr.Body.String(), expected)
	}
}

func Test_healthCheckPage(t *testing.T) {
	req, err := http.NewRequest("GET", "/health", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(healthCheckPage)

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}

	healthStatus := `"alive": "true"`
	if !strings.Contains(rr.Body.String(), healthStatus) {
		t.Errorf("handler returned unexpected body: got %v want %v",
			rr.Body.String(), healthStatus)
	}
}

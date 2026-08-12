package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuildMux_RoutesBothContractPaths(t *testing.T) {
	turn := func(context.Context, string, map[string]any) (any, error) {
		return "ok", nil
	}
	stream := staticStream(t, "", "ok")
	mux := buildMux(turn, stream)

	for _, route := range []string{routeUnary, routeStream} {
		t.Run(route, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, route,
				strings.NewReader(`{"input":{"message":"hi"}}`))

			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestBuildMux_UnknownRouteIs404(t *testing.T) {
	turn := func(context.Context, string, map[string]any) (any, error) { return "ok", nil }
	mux := buildMux(turn, staticStream(t, "", "ok"))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/nope", strings.NewReader(`{}`))

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

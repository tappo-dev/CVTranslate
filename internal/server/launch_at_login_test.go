package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

type fakeLaunchAtLogin struct {
	enabled  bool
	writeErr error
	writes   int
}

func (launch *fakeLaunchAtLogin) Enabled() (bool, error) { return launch.enabled, nil }

func (launch *fakeLaunchAtLogin) SetEnabled(enabled bool) error {
	launch.writes++
	if launch.writeErr != nil {
		return launch.writeErr
	}
	launch.enabled = enabled
	return nil
}

func TestLaunchAtLoginAppliesImmediatelyWithoutChangingSettings(test *testing.T) {
	application := newIntegrationServer(test)
	launch := &fakeLaunchAtLogin{}
	application.options.LaunchAtLogin = launch
	before, err := application.store.GetSettings(context.Background())
	if err != nil {
		test.Fatal(err)
	}
	for _, enabled := range []bool{true, false} {
		body := `{"enabled":false}`
		if enabled {
			body = `{"enabled":true}`
		}
		response := performRequest(application, "PUT", "/api/launch-at-login", body, testOrigin)
		if response.Code != http.StatusOK || launch.enabled != enabled ||
			strings.TrimSpace(response.Body.String()) != body {
			test.Fatalf("enabled=%v returned %d: %s", enabled, response.Code, response.Body.String())
		}
	}
	after, _ := application.store.GetSettings(context.Background())
	if after != before {
		test.Fatal("login item change rewrote persisted settings")
	}
}

func TestLaunchAtLoginRejectsInvalidUnavailableAndFailedRequests(test *testing.T) {
	application := newIntegrationServer(test)
	response := performRequest(application, "PUT", "/api/launch-at-login", `{"enabled":true}`, testOrigin)
	if response.Code != http.StatusServiceUnavailable {
		test.Fatalf("unavailable login item returned %d", response.Code)
	}
	launch := &fakeLaunchAtLogin{}
	application.options.LaunchAtLogin = launch
	for _, body := range []string{`{}`, `{"enabled":"yes"}`, `{"enabled":true,"path":"x"}`} {
		if response = performRequest(application, "PUT", "/api/launch-at-login", body, testOrigin); response.Code != http.StatusBadRequest {
			test.Fatalf("%s returned %d", body, response.Code)
		}
	}
	if response = performRequest(application, "PUT", "/api/launch-at-login", `{"enabled":true}`, ""); response.Code != http.StatusForbidden {
		test.Fatalf("mutation without Origin returned %d", response.Code)
	}
	if launch.writes != 0 {
		test.Fatal("rejected requests changed the login item")
	}
	launch.writeErr = errors.New("registry denied")
	response = performRequest(application, "PUT", "/api/launch-at-login", `{"enabled":true}`, testOrigin)
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "registry denied") {
		test.Fatalf("failed write returned %d: %s", response.Code, response.Body.String())
	}
}

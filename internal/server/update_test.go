package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"testing"

	"cvtranslate/internal/storage"
	"cvtranslate/internal/update"
)

type updateFunction func(context.Context, string) (update.Result, error)

func (check updateFunction) Check(ctx context.Context, currentVersion string) (update.Result, error) {
	return check(ctx, currentVersion)
}

func newUpdateServer(test *testing.T, checker UpdateChecker) *Server {
	test.Helper()
	repository, err := storage.Open(filepath.Join(test.TempDir(), "update.sqlite"))
	if err != nil {
		test.Fatal(err)
	}
	test.Cleanup(func() { _ = repository.Close() })
	return New(repository, nil, Options{Origin: testOrigin, Version: "0.1.0", Updates: checker})
}

func TestUpdateCheckRequiresLocalOriginAndPassesVersion(test *testing.T) {
	calls := 0
	application := newUpdateServer(test, updateFunction(func(_ context.Context, currentVersion string) (update.Result, error) {
		calls++
		return update.Result{CurrentVersion: currentVersion, LatestVersion: "0.2.0", Status: update.StatusUpdateAvailable, ReleaseURL: update.RepositoryURL + "/releases/tag/v0.2.0"}, nil
	}))
	for _, origin := range []string{"", "https://attacker.example"} {
		if response := performRequest(application, "POST", "/api/update-check", "", origin); response.Code != http.StatusForbidden {
			test.Fatalf("origin %q returned %d", origin, response.Code)
		}
	}
	if calls != 0 {
		test.Fatal("rejected request reached GitHub")
	}
	response := performRequest(application, "POST", "/api/update-check", "", testOrigin)
	var result update.Result
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil ||
		result.CurrentVersion != "0.1.0" || result.Status != update.StatusUpdateAvailable || calls != 1 {
		test.Fatalf("unexpected response %d: %s", response.Code, response.Body.String())
	}
}

func TestUpdateCheckFailuresUseSafeErrors(test *testing.T) {
	for _, failureCase := range []struct {
		err    error
		status int
		code   string
	}{
		{&update.Error{Code: "network_failed", Message: "Could not connect to GitHub."}, http.StatusBadGateway, "network_failed"},
		{&update.Error{Code: "rate_limited", Message: "Rate limited."}, http.StatusTooManyRequests, "rate_limited"},
		{&update.Error{Code: "timeout", Message: "Timed out."}, http.StatusGatewayTimeout, "timeout"},
		{errors.New("dial tcp: private detail"), http.StatusBadGateway, "update_check_failed"},
	} {
		application := newUpdateServer(test, updateFunction(func(context.Context, string) (update.Result, error) {
			return update.Result{}, failureCase.err
		}))
		response := performRequest(application, "POST", "/api/update-check", "", testOrigin)
		var payload struct {
			Error struct{ Code, Message string } `json:"error"`
		}
		if response.Code != failureCase.status || json.Unmarshal(response.Body.Bytes(), &payload) != nil ||
			payload.Error.Code != failureCase.code || payload.Error.Message == "dial tcp: private detail" {
			test.Fatalf("unexpected failure response %d: %s", response.Code, response.Body.String())
		}
	}
	unavailable := newUpdateServer(test, nil)
	if response := performRequest(unavailable, "POST", "/api/update-check", "", testOrigin); response.Code != http.StatusServiceUnavailable {
		test.Fatalf("missing checker returned %d", response.Code)
	}
}

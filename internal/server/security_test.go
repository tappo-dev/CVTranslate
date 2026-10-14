package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const testOrigin = "http://127.0.0.1:19327"

func performRequest(application *Server, method, path, body, origin string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, testOrigin+path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	response := httptest.NewRecorder()
	application.ServeHTTP(response, request)
	return response
}

func TestLocalAPIWorksWithoutBrowserCredentials(test *testing.T) {
	for _, development := range []bool{false, true} {
		application := newIntegrationServer(test)
		application.options.Development = development
		for _, requestCase := range []struct {
			method, path, origin string
			status               int
		}{
			{method: "GET", path: "/api/state", status: http.StatusOK},
			{method: "GET", path: "/api/history", status: http.StatusOK},
			{method: "GET", path: "/api/tasks/missing", status: http.StatusNotFound},
		} {
			response := performRequest(application, requestCase.method, requestCase.path, "", requestCase.origin)
			if response.Code != requestCase.status {
				test.Fatalf("development=%v %s %s returned %d: %s", development, requestCase.method, requestCase.path, response.Code, response.Body.String())
			}
			if len(response.Result().Cookies()) != 0 || response.Header().Get("Cache-Control") != "no-store" {
				test.Fatal("local API issued a cookie or allowed response caching")
			}
		}
	}
}

func TestBrowserAPIHasNoApplicationShutdownEndpoint(test *testing.T) {
	application := newIntegrationServer(test)
	response := performRequest(application, http.MethodPost, "/api/shutdown", `{}`, testOrigin)
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), "not_found") {
		test.Fatalf("shutdown endpoint returned %d: %s", response.Code, response.Body.String())
	}
}

func TestLocalOriginBoundaries(test *testing.T) {
	application := New(nil, nil, Options{Origin: testOrigin})
	for _, method := range []string{"POST", "PUT", "DELETE", "PATCH"} {
		for _, origin := range []string{"", "https://attacker.example", "null", "http://127.0.0.1:19329"} {
			response := performRequest(application, method, "/api/shutdown", `{}`, origin)
			if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "invalid_origin") {
				test.Fatalf("%s with origin %q returned %d", method, origin, response.Code)
			}
			if response.Header().Get("Access-Control-Allow-Origin") != "" {
				test.Fatal("foreign origin received CORS permission")
			}
		}
	}
	for _, requestCase := range []struct {
		name, address, origin, fetchSite string
	}{
		{name: "DNS rebinding", address: "http://attacker.example:19327/api/state"},
		{name: "wrong port", address: "http://127.0.0.1:19329/api/state"},
		{name: "foreign origin read", origin: "https://attacker.example"},
		{name: "opaque origin read", origin: "null"},
		{name: "cross-site read without origin", fetchSite: "cross-site"},
		{name: "same-site cross-origin read", fetchSite: "same-site"},
	} {
		test.Run(requestCase.name, func(test *testing.T) {
			address := requestCase.address
			if address == "" {
				address = testOrigin + "/api/state"
			}
			request := httptest.NewRequest("GET", address, nil)
			request.Header.Set("Origin", requestCase.origin)
			request.Header.Set("Sec-Fetch-Site", requestCase.fetchSite)
			response := httptest.NewRecorder()
			application.ServeHTTP(response, request)
			if response.Code != http.StatusForbidden {
				test.Fatalf("untrusted read returned %d", response.Code)
			}
		})
	}
}

func TestDirectAndSameOriginPageVisits(test *testing.T) {
	application := New(nil, nil, Options{
		Origin: testOrigin,
		Assets: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusOK)
		}),
	})
	for _, fetchSite := range []string{"", "none", "same-origin"} {
		request := httptest.NewRequest("GET", testOrigin+"/?page=translate", nil)
		request.Header.Set("Sec-Fetch-Site", fetchSite)
		response := httptest.NewRecorder()
		application.ServeHTTP(response, request)
		if response.Code != http.StatusOK || len(response.Result().Cookies()) != 0 {
			test.Fatalf("direct page visit with fetch site %q returned %d", fetchSite, response.Code)
		}
	}
}

func TestInstanceReopenStillRequiresControlToken(test *testing.T) {
	var openedAddress string
	application := New(nil, nil, Options{
		Origin: testOrigin, ControlToken: "private-control",
		OpenBrowser: func(address string) error { openedAddress = address; return nil },
	})
	for _, token := range []string{"", "wrong-control", "private-control"} {
		request := httptest.NewRequest("POST", testOrigin+"/internal/open", nil)
		request.Header.Set("X-Instance-Token", token)
		response := httptest.NewRecorder()
		application.ServeHTTP(response, request)
		if token != "private-control" {
			if response.Code != http.StatusForbidden || openedAddress != "" {
				test.Fatal("instance reopened without its control token")
			}
		} else if response.Code != http.StatusOK || openedAddress != testOrigin+"/" {
			test.Fatalf("authorized instance reopen failed: %d %q", response.Code, openedAddress)
		}
		if strings.Contains(response.Body.String(), "private-control") {
			test.Fatal("control endpoint disclosed its token")
		}
	}
}

func TestOpenUIUsesReusableTaskLinks(test *testing.T) {
	var openedAddress string
	application := New(nil, nil, Options{
		Origin:      testOrigin,
		OpenBrowser: func(address string) error { openedAddress = address; return nil },
	})
	for _, taskID := range []string{"", "task-123", "task &?/#"} {
		if err := application.OpenUI(taskID); err != nil {
			test.Fatal(err)
		}
		address, err := url.Parse(openedAddress)
		if err != nil {
			test.Fatal(err)
		}
		if address.Scheme+"://"+address.Host != testOrigin || address.Path != "/" || address.Fragment != "" || address.Query().Get("task") != taskID {
			test.Fatalf("invalid local task link: %q", openedAddress)
		}
		if openedAddress != application.BrowserURL(taskID) {
			test.Fatal("reopening changed the reusable link")
		}
	}
}

func TestSessionBootstrapEndpointRemoved(test *testing.T) {
	application := New(nil, nil, Options{Origin: testOrigin})
	for _, method := range []string{"GET", "POST"} {
		response := performRequest(application, method, "/api/session", `{}`, testOrigin)
		if response.Code != http.StatusNotFound || len(response.Result().Cookies()) != 0 {
			test.Fatalf("obsolete session endpoint returned %d", response.Code)
		}
	}
}

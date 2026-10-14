package update

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCompareVersionsFollowsSemanticVersioning(test *testing.T) {
	for _, comparisonCase := range []struct {
		left, right string
		expected    int
	}{
		{"0.10.0", "0.9.0", 1},
		{"v1.0.0", "1.0.0", 0},
		{"1.0.0", "1.0.0-rc.1", 1},
		{"1.0.0-rc.2", "1.0.0-rc.10", -1},
		{"1.0.0-alpha", "1.0.0-alpha.1", -1},
		{"1.0.0-alpha.beta", "1.0.0-alpha.1", 1},
		{"1.0.0+build.5", "1.0.0", 0},
		{"2.0.0", "10.0.0", -1},
	} {
		if actual := compareVersions(comparisonCase.left, comparisonCase.right); actual != comparisonCase.expected {
			test.Errorf("compareVersions(%q, %q) = %d, expected %d", comparisonCase.left, comparisonCase.right, actual, comparisonCase.expected)
		}
	}
}

func newTestChecker(test *testing.T, handler http.HandlerFunc) *Checker {
	test.Helper()
	upstream := httptest.NewServer(handler)
	test.Cleanup(upstream.Close)
	checker := New()
	checker.endpoint = upstream.URL
	checker.httpClient.Transport = upstream.Client().Transport
	return checker
}

func TestCheckReportsReleaseStatus(test *testing.T) {
	for _, statusCase := range []struct {
		name, tag, current, status, latest, releaseURL string
	}{
		{"newer release", "v0.2.0", "0.1.0", StatusUpdateAvailable, "0.2.0", RepositoryURL + "/releases/tag/v0.2.0"},
		{"same release", "v0.1.0", "0.1.0", StatusUpToDate, "0.1.0", RepositoryURL + "/releases/tag/v0.1.0"},
		{"older release", "0.0.9", "0.1.0", StatusUpToDate, "0.0.9", RepositoryURL + "/releases/tag/0.0.9"},
		{"development build", "v0.2.0", "dev", StatusNotComparable, "0.2.0", RepositoryURL + "/releases/tag/v0.2.0"},
	} {
		test.Run(statusCase.name, func(test *testing.T) {
			checker := newTestChecker(test, func(writer http.ResponseWriter, request *http.Request) {
				if request.Header.Get("User-Agent") == "" || request.URL.RawQuery != "" {
					test.Errorf("unexpected request: %v", request.URL)
				}
				_, _ = writer.Write([]byte(`{"tag_name":"` + statusCase.tag + `","html_url":"https://attacker.example/"}`))
			})
			result, err := checker.Check(context.Background(), statusCase.current)
			if err != nil {
				test.Fatal(err)
			}
			if result.Status != statusCase.status || result.LatestVersion != statusCase.latest ||
				result.CurrentVersion != statusCase.current || result.ReleaseURL != statusCase.releaseURL {
				test.Fatalf("unexpected result: %+v", result)
			}
		})
	}
}

func TestCheckWithoutPublishedRelease(test *testing.T) {
	checker := newTestChecker(test, func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNotFound)
	})
	result, err := checker.Check(context.Background(), "0.1.0")
	if err != nil || result.Status != StatusNoRelease || result.LatestVersion != "" || result.ReleaseURL != RepositoryURL+"/releases" {
		test.Fatalf("unexpected result: %+v %v", result, err)
	}
}

func TestCheckFailuresAreClassified(test *testing.T) {
	for _, failureCase := range []struct {
		name    string
		handler http.HandlerFunc
		code    string
	}{
		{"rate limited", func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("X-RateLimit-Remaining", "0")
			writer.WriteHeader(http.StatusForbidden)
		}, "rate_limited"},
		{"server error", func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusBadGateway)
		}, "upstream_failed"},
		{"redirect not followed", func(writer http.ResponseWriter, request *http.Request) {
			http.Redirect(writer, request, "https://attacker.example/", http.StatusFound)
		}, "upstream_failed"},
		{"malformed json", func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = writer.Write([]byte(`not json`))
		}, "invalid_response"},
		{"unrecognized tag", func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = writer.Write([]byte(`{"tag_name":"latest<script>"}`))
		}, "invalid_response"},
		{"oversized body", func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = writer.Write([]byte(`{"tag_name":"v1.0.0","body":"` + strings.Repeat("x", maxResponseBytes) + `"}`))
		}, "invalid_response"},
	} {
		test.Run(failureCase.name, func(test *testing.T) {
			_, err := newTestChecker(test, failureCase.handler).Check(context.Background(), "0.1.0")
			var checkError *Error
			if !errors.As(err, &checkError) || checkError.Code != failureCase.code {
				test.Fatalf("expected %s, got %v", failureCase.code, err)
			}
		})
	}
}

func TestCheckReportsUnreachableAndCancelled(test *testing.T) {
	unreachable := New()
	unreachable.endpoint = "http://127.0.0.1:1/unreachable"
	_, err := unreachable.Check(context.Background(), "0.1.0")
	var checkError *Error
	if !errors.As(err, &checkError) || checkError.Code != "network_failed" {
		test.Fatalf("expected network_failed, got %v", err)
	}

	slow := newTestChecker(test, func(_ http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = slow.Check(ctx, "0.1.0")
	if !errors.As(err, &checkError) || checkError.Code != "timeout" {
		test.Fatalf("expected timeout, got %v", err)
	}

	cancelledContext, cancelNow := context.WithCancel(context.Background())
	cancelNow()
	_, err = slow.Check(cancelledContext, "0.1.0")
	if !errors.As(err, &checkError) || checkError.Code != "cancelled" {
		test.Fatalf("expected cancelled, got %v", err)
	}
}

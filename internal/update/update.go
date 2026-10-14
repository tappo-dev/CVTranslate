// Package update performs explicit, on-demand release checks against GitHub.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const RepositoryURL = "https://github.com/tappo-dev/CVTranslate"
const latestReleaseAPI = "https://api.github.com/repos/tappo-dev/CVTranslate/releases/latest"
const checkTimeout = 10 * time.Second
const maxResponseBytes = 256 * 1024

const (
	StatusUpdateAvailable = "update_available"
	StatusUpToDate        = "up_to_date"
	StatusNoRelease       = "no_release"
	// StatusNotComparable is used when the running build has no release version, such as development builds.
	StatusNotComparable = "not_comparable"
)

var releaseTagPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)

type Result struct {
	CurrentVersion string `json:"currentVersion"`
	LatestVersion  string `json:"latestVersion"`
	Status         string `json:"status"`
	ReleaseURL     string `json:"releaseUrl"`
}

type Error struct {
	Code    string
	Message string
}

func (failure *Error) Error() string { return failure.Message }

func failure(code, message string) error {
	return &Error{Code: code, Message: message}
}

// Checker sends no identifiers, version or usage data; GitHub sees only the request itself.
type Checker struct {
	httpClient *http.Client
	endpoint   string
}

func New() *Checker {
	return &Checker{
		httpClient: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
		endpoint: latestReleaseAPI,
	}
}

func (checker *Checker) Check(ctx context.Context, currentVersion string) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, checker.endpoint, nil)
	if err != nil {
		return Result{}, failure("update_check_failed", "Could not prepare the update check.")
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "CVTranslate-update-check")
	response, err := checker.httpClient.Do(request)
	if err != nil {
		return Result{}, classifyTransportError(ctx, err)
	}
	defer response.Body.Close()
	result := Result{CurrentVersion: currentVersion, ReleaseURL: RepositoryURL + "/releases"}
	switch {
	case response.StatusCode == http.StatusNotFound:
		result.Status = StatusNoRelease
		return result, nil
	case response.StatusCode == http.StatusTooManyRequests ||
		response.StatusCode == http.StatusForbidden && response.Header.Get("X-RateLimit-Remaining") == "0":
		return Result{}, failure("rate_limited", "GitHub rate limit reached. Try again later.")
	case response.StatusCode != http.StatusOK:
		return Result{}, failure("upstream_failed", "GitHub returned HTTP "+strconv.Itoa(response.StatusCode)+".")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return Result{}, classifyTransportError(ctx, err)
	}
	if len(body) > maxResponseBytes {
		return Result{}, failure("invalid_response", "GitHub returned an oversized release response.")
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if json.Unmarshal(body, &release) != nil || !releaseTagPattern.MatchString(release.TagName) {
		return Result{}, failure("invalid_response", "GitHub returned an unrecognized release version.")
	}
	result.LatestVersion = strings.TrimPrefix(release.TagName, "v")
	// Build the page link locally rather than trusting a URL from the response.
	result.ReleaseURL = RepositoryURL + "/releases/tag/" + url.PathEscape(release.TagName)
	if !releaseTagPattern.MatchString(currentVersion) {
		result.Status = StatusNotComparable
	} else if compareVersions(result.LatestVersion, currentVersion) > 0 {
		result.Status = StatusUpdateAvailable
	} else {
		result.Status = StatusUpToDate
	}
	return result, nil
}

func classifyTransportError(ctx context.Context, err error) error {
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return failure("timeout", "GitHub did not respond in time.")
	case errors.Is(ctx.Err(), context.Canceled):
		return failure("cancelled", "The update check was cancelled.")
	default:
		return failure("network_failed", "Could not connect to GitHub.")
	}
}

// compareVersions orders two Semantic Versioning 2.0.0 strings that already
// match releaseTagPattern, ignoring an optional "v" prefix and build metadata.
func compareVersions(left, right string) int {
	leftParts := releaseTagPattern.FindStringSubmatch(left)
	rightParts := releaseTagPattern.FindStringSubmatch(right)
	for index := 1; index <= 3; index++ {
		if comparison := compareNumericIdentifiers(leftParts[index], rightParts[index]); comparison != 0 {
			return comparison
		}
	}
	return comparePrerelease(leftParts[4], rightParts[4])
}

func compareNumericIdentifiers(left, right string) int {
	if len(left) != len(right) {
		if len(left) < len(right) {
			return -1
		}
		return 1
	}
	return strings.Compare(left, right)
}

func comparePrerelease(left, right string) int {
	switch {
	case left == right:
		return 0
	case left == "":
		return 1
	case right == "":
		return -1
	}
	leftIdentifiers := strings.Split(left, ".")
	rightIdentifiers := strings.Split(right, ".")
	for index := 0; index < len(leftIdentifiers) && index < len(rightIdentifiers); index++ {
		leftIdentifier, rightIdentifier := leftIdentifiers[index], rightIdentifiers[index]
		leftNumeric, rightNumeric := isNumericIdentifier(leftIdentifier), isNumericIdentifier(rightIdentifier)
		var comparison int
		switch {
		case leftNumeric && rightNumeric:
			comparison = compareNumericIdentifiers(leftIdentifier, rightIdentifier)
		case leftNumeric:
			comparison = -1
		case rightNumeric:
			comparison = 1
		default:
			comparison = strings.Compare(leftIdentifier, rightIdentifier)
		}
		if comparison != 0 {
			return comparison
		}
	}
	return compareNumericIdentifiers(strconv.Itoa(len(leftIdentifiers)), strconv.Itoa(len(rightIdentifiers)))
}

func isNumericIdentifier(identifier string) bool {
	for _, character := range identifier {
		if character < '0' || character > '9' {
			return false
		}
	}
	return identifier != ""
}

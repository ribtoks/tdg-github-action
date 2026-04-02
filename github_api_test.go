package main

import (
	"context"
	errors "errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/go-github/v73/github"
	"github.com/jpillora/backoff"
)

func TestGitHubAPIRetryRetriesTransientErrors(t *testing.T) {
	attempts := 0
	api := &githubAPI{
		times: 3,
		newBackoff: func() *backoff.Backoff {
			return &backoff.Backoff{Min: time.Millisecond, Max: time.Millisecond, Factor: 2}
		},
		wait: func(context.Context, time.Duration) error {
			return nil
		},
	}

	err := api.retry(context.Background(), "issues.create", func() error {
		attempts++
		if attempts < 3 {
			return &github.ErrorResponse{
				Response: &http.Response{StatusCode: http.StatusBadGateway},
			}
		}

		return nil
	})

	if err != nil {
		t.Fatalf("retry() error = %v, want nil", err)
	}

	if attempts != 3 {
		t.Fatalf("retry() attempts = %d, want 3", attempts)
	}
}

func TestGitHubAPIRetryStopsOnNonRetryableError(t *testing.T) {
	attempts := 0
	api := &githubAPI{
		times: 3,
		newBackoff: func() *backoff.Backoff {
			return &backoff.Backoff{Min: time.Millisecond, Max: time.Millisecond, Factor: 2}
		},
		wait: func(context.Context, time.Duration) error {
			return nil
		},
	}

	want := errors.New("boom")
	err := api.retry(context.Background(), "issues.create", func() error {
		attempts++
		return want
	})

	if !errors.Is(err, want) {
		t.Fatalf("retry() error = %v, want %v", err, want)
	}

	if attempts != 1 {
		t.Fatalf("retry() attempts = %d, want 1", attempts)
	}
}

func TestGitHubAPIRetryReturnsContextErrorWhileWaiting(t *testing.T) {
	api := &githubAPI{
		times: 3,
		newBackoff: func() *backoff.Backoff {
			return &backoff.Backoff{Min: time.Millisecond, Max: time.Millisecond, Factor: 2}
		},
		wait: waitForRetry,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := api.retry(ctx, "issues.create", func() error {
		return &github.ErrorResponse{
			Response: &http.Response{StatusCode: http.StatusServiceUnavailable},
		}
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("retry() error = %v, want %v", err, context.Canceled)
	}
}

func TestIsRetryableGitHubError(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/o/r/issues", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	rateLimitResp := &http.Response{
		StatusCode: http.StatusForbidden,
		Request:    req,
	}

	abuseResp := &http.Response{
		StatusCode: http.StatusForbidden,
		Request:    req,
	}

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil",
			err:  nil,
			want: false,
		},
		{
			name: "rate limit",
			err: &github.RateLimitError{
				Response: rateLimitResp,
			},
			want: true,
		},
		{
			name: "abuse limit",
			err: &github.AbuseRateLimitError{
				Response: abuseResp,
			},
			want: true,
		},
		{
			name: "accepted",
			err:  &github.AcceptedError{},
			want: true,
		},
		{
			name: "server error",
			err: &github.ErrorResponse{
				Response: &http.Response{StatusCode: http.StatusBadGateway},
			},
			want: true,
		},
		{
			name: "too many requests",
			err: &github.ErrorResponse{
				Response: &http.Response{StatusCode: http.StatusTooManyRequests},
			},
			want: true,
		},
		{
			name: "bad request",
			err: &github.ErrorResponse{
				Response: &http.Response{StatusCode: http.StatusBadRequest},
			},
			want: false,
		},
		{
			name: "network timeout",
			err: &url.Error{
				Op:  "Get",
				URL: "https://api.github.com",
				Err: timeoutError{},
			},
			want: true,
		},
		{
			name: "plain error",
			err:  errors.New("plain"),
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRetryableGitHubError(tc.err); got != tc.want {
				t.Fatalf("isRetryableGitHubError() = %v, want %v", got, tc.want)
			}
		})
	}
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

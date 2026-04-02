package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/google/go-github/v73/github"
	"github.com/jpillora/backoff"
)

const (
	defaultGitHubAPIRetries     = 4
	defaultGitHubAPIRetryMin    = time.Second
	defaultGitHubAPIRetryMax    = 10 * time.Second
	defaultGitHubAPIRetryFactor = 2
)

type githubAPI struct {
	client     *github.Client
	times      int
	newBackoff func() *backoff.Backoff
	wait       func(context.Context, time.Duration) error
}

func newGitHubAPI(client *github.Client) *githubAPI {
	return &githubAPI{
		client: client,
		times:  defaultGitHubAPIRetries,
		newBackoff: func() *backoff.Backoff {
			return &backoff.Backoff{
				Min:    defaultGitHubAPIRetryMin,
				Max:    defaultGitHubAPIRetryMax,
				Factor: defaultGitHubAPIRetryFactor,
				Jitter: true,
			}
		},
		wait: waitForRetry,
	}
}

func waitForRetry(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (g *githubAPI) listByRepo(ctx context.Context, owner, repo string, opt *github.IssueListByRepoOptions) ([]*github.Issue, *github.Response, error) {
	var (
		issues []*github.Issue
		resp   *github.Response
	)

	err := g.retry(ctx, "issues.list_by_repo", func() error {
		var err error
		issues, resp, err = g.doListByRepo(ctx, owner, repo, opt)
		return err
	})

	return issues, resp, err
}

func (g *githubAPI) doListByRepo(ctx context.Context, owner, repo string, opt *github.IssueListByRepoOptions) ([]*github.Issue, *github.Response, error) {
	return g.client.Issues.ListByRepo(ctx, owner, repo, opt)
}

func (g *githubAPI) createIssue(ctx context.Context, owner, repo string, issue *github.IssueRequest) (*github.Issue, *github.Response, error) {
	var (
		created *github.Issue
		resp    *github.Response
	)

	err := g.retry(ctx, "issues.create", func() error {
		var err error
		created, resp, err = g.doCreateIssue(ctx, owner, repo, issue)
		return err
	})

	return created, resp, err
}

func (g *githubAPI) doCreateIssue(ctx context.Context, owner, repo string, issue *github.IssueRequest) (*github.Issue, *github.Response, error) {
	return g.client.Issues.Create(ctx, owner, repo, issue)
}

func (g *githubAPI) editIssue(ctx context.Context, owner, repo string, number int, issue *github.IssueRequest) (*github.Issue, *github.Response, error) {
	var (
		edited *github.Issue
		resp   *github.Response
	)

	err := g.retry(ctx, "issues.edit", func() error {
		var err error
		edited, resp, err = g.doEditIssue(ctx, owner, repo, number, issue)
		return err
	})

	return edited, resp, err
}

func (g *githubAPI) doEditIssue(ctx context.Context, owner, repo string, number int, issue *github.IssueRequest) (*github.Issue, *github.Response, error) {
	return g.client.Issues.Edit(ctx, owner, repo, number, issue)
}

func (g *githubAPI) getCommit(ctx context.Context, owner, repo, sha string, opt *github.ListOptions) (*github.RepositoryCommit, *github.Response, error) {
	var (
		commit *github.RepositoryCommit
		resp   *github.Response
	)

	err := g.retry(ctx, "repositories.get_commit", func() error {
		var err error
		commit, resp, err = g.doGetCommit(ctx, owner, repo, sha, opt)
		return err
	})

	return commit, resp, err
}

func (g *githubAPI) doGetCommit(ctx context.Context, owner, repo, sha string, opt *github.ListOptions) (*github.RepositoryCommit, *github.Response, error) {
	return g.client.Repositories.GetCommit(ctx, owner, repo, sha, opt)
}

func (g *githubAPI) createComment(ctx context.Context, owner, repo string, number int, comment *github.IssueComment) (*github.IssueComment, *github.Response, error) {
	var (
		created *github.IssueComment
		resp    *github.Response
	)

	err := g.retry(ctx, "issues.create_comment", func() error {
		var err error
		created, resp, err = g.doCreateComment(ctx, owner, repo, number, comment)
		return err
	})

	return created, resp, err
}

func (g *githubAPI) doCreateComment(ctx context.Context, owner, repo string, number int, comment *github.IssueComment) (*github.IssueComment, *github.Response, error) {
	return g.client.Issues.CreateComment(ctx, owner, repo, number, comment)
}

func (g *githubAPI) retry(ctx context.Context, operation string, fn func() error) error {
	b := g.newBackoff()
	var err error

	for attempt := 0; attempt < g.times; attempt++ {
		if attempt > 0 {
			delay := b.Duration()
			if waitErr := g.wait(ctx, delay); waitErr != nil {
				return waitErr
			}
		}

		err = fn()
		if !isRetryableGitHubError(err) {
			return err
		}

		if attempt == g.times-1 {
			return err
		}

		log.Printf("Retrying GitHub API call. operation=%s attempt=%d err=%v", operation, attempt+1, err)
	}

	return err
}

func isRetryableGitHubError(err error) bool {
	if err == nil {
		return false
	}

	var abuseErr *github.AbuseRateLimitError
	if errors.As(err, &abuseErr) {
		return true
	}

	var rateLimitErr *github.RateLimitError
	if errors.As(err, &rateLimitErr) {
		return true
	}

	var acceptedErr *github.AcceptedError
	if errors.As(err, &acceptedErr) {
		return true
	}

	var responseErr *github.ErrorResponse
	if errors.As(err, &responseErr) && responseErr.Response != nil {
		status := responseErr.Response.StatusCode
		return status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
	}

	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/google/go-github/v31/github"
	"github.com/ribtoks/tdg/pkg/tdglib"
	"golang.org/x/oauth2"
)

const (
	defaultMinWords      = 3
	defaultMinChars      = 30
	defaultIssuesPerPage = 100
	contextLinesUp       = 3
	contextLinesDown     = 7
	ghRoot               = "/github/workspace"
)

func sourceRoot(root string) string {
	if strings.HasPrefix(root, "/") {
		return ghRoot + root
	}

	return fmt.Sprintf("%s/%v", ghRoot, root)
}

type service struct {
	ctx    context.Context
	owner  string
	repo   string
	label  string
	sha    string
	client *github.Client
	dryRun bool
}

func (s *service) fetchGithubIssues() ([]*github.Issue, error) {
	var allIssues []*github.Issue

	opt := &github.IssueListByRepoOptions{
		ListOptions: github.ListOptions{PerPage: defaultIssuesPerPage},
	}

	for {
		issues, resp, err := s.client.Issues.ListByRepo(s.ctx, s.owner, s.repo, opt)
		if err != nil {
			return nil, err
		}

		allIssues = append(allIssues, issues...)

		if resp.NextPage == 0 {
			break
		}

		opt.Page = resp.NextPage
	}
	log.Printf("Fetched github issues. count=%v", len(allIssues))

	return allIssues, nil
}

func (s *service) createFileLink(c *tdglib.ToDoComment) string {
	start := c.Line - contextLinesUp
	if start < 0 {
		start = 0
	}

	end := c.Line + contextLinesDown
	// https://github.com/{repo}/blob/{sha}{file}#L{startLines}-L{endLine}
	return fmt.Sprintf("https://github.com/%s/%s/blob/%s/%s#L%v-L%v", s.owner, s.repo, s.sha, c.File, start, end)
}

func (s *service) openNewIssues(issueMap map[string]*github.Issue, comments []*tdglib.ToDoComment) error {
	for _, c := range comments {
		_, ok := issueMap[c.Title]
		if !ok {
			body := c.Body
			body += fmt.Sprintf("\n\nLine: %v\n%s", c.Line, s.createFileLink(c))

			log.Printf("About to create an issue. title=%v body=%v", c.Title, c.Body)

			if s.dryRun {
				log.Printf("Dry run mode.")
				continue
			}

			labels := []string{s.label}

			issue := &github.IssueRequest{
				Title:  &c.Title,
				Body:   &body,
				Labels: &labels,
			}

			if _, _, err := s.client.Issues.Create(s.ctx, s.owner, s.repo, issue); err != nil {
				return err
			}
		}
	}

	return nil
}

func main() {
	log.SetOutput(os.Stdout)

	r := strings.Split(os.Getenv("INPUT_REPO"), "/")
	owner, repo := r[0], r[1]
	label := os.Getenv("INPUT_LABEL")
	token := os.Getenv("INPUT_TOKEN")
	sha := os.Getenv("INPUT_SHA")
	includePattern := os.Getenv("INPUT_INCLUDE_PATTERN")
	excludePattern := os.Getenv("INPUT_EXCLUDE_PATTERN")
	srcRoot := sourceRoot(os.Getenv("INPUT_ROOT"))
	dryRun := len(os.Getenv("INPUT_DRY_RUN")) > 0

	minWords, err := strconv.Atoi(os.Getenv("INPUT_MIN_WORDS"))
	if err != nil {
		minWords = defaultMinWords
	}

	minChars, err := strconv.Atoi(os.Getenv("INPUT_MIN_CHARACTERS"))
	if err != nil {
		minChars = defaultMinChars
	}

	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)

	svc := &service{
		ctx:    ctx,
		client: github.NewClient(tc),
		owner:  owner,
		repo:   repo,
		dryRun: dryRun,
		label:  label,
		sha:    sha,
	}

	issues, err := svc.fetchGithubIssues()
	if err != nil {
		log.Panic(err)
	}

	includePatterns := make([]string, 0)
	if len(includePattern) > 0 {
		includePatterns = append(includePatterns, includePattern)
	}

	excludePatterns := make([]string, 0)
	if len(excludePattern) > 0 {
		excludePatterns = append(excludePatterns, excludePattern)
	}
	//env := tdglib.NewEnvironment(srcRoot)
	td := tdglib.NewToDoGenerator(srcRoot,
		includePatterns,
		excludePatterns,
		minWords,
		minChars)

	comments, err := td.Generate()
	if err != nil {
		log.Panic(err)
	}

	log.Printf("Extracted TODO comments. count=%v", len(comments))

	issueMap := make(map[string]*github.Issue)
	for _, i := range issues {
		issueMap[i.GetTitle()] = i
	}

	err = svc.openNewIssues(issueMap, comments)
	if err != nil {
		log.Panic(err)
	}
}

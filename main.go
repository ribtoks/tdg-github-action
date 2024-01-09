package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/google/go-github/v49/github"
	"gitlab.com/ribtoks/tdg/pkg/tdglib"
	"golang.org/x/oauth2"
)

const (
	defaultMinWords      = 3
	defaultMinChars      = 30
	defaultAddLimit      = 0
	defaultCloseLimit    = 0
	defaultIssuesPerPage = 200
	defaultConcurrency   = 128
	contextLinesUp       = 3
	contextLinesDown     = 7
	ghRoot               = "/github/workspace"
	minEstimate          = 0.01
	hourMinutes          = 60
	labelBranchPrefix    = "branch: "
	labelTypePrefix      = "type: "
	labelAreaPrefix      = "area: "
)

func sourceRoot(root string) string {
	if strings.HasPrefix(root, "/") {
		return ghRoot + root
	}

	return fmt.Sprintf("%s/%v", ghRoot, root)
}

type env struct {
	root              string
	owner             string
	repo              string
	label             string
	token             string
	sha               string
	ref               string
	branch            string
	includeRE         string
	excludeRE         string
	projectColumnID   int64
	minWords          int
	minChars          int
	addLimit          int
	closeLimit        int
	concurrency       int
	closeOnSameBranch bool
	extendedLabels    bool
	dryRun            bool
	commentIssue      bool
	assignFromBlame   bool
}

type service struct {
	ctx    context.Context
	client *github.Client
	env    *env
	wg     sync.WaitGroup
}

func (e *env) sourceRoot() string {
	return sourceRoot(e.root)
}

func flagToBool(s string) bool {
	s = strings.ToLower(s)
	return s == "1" || s == "true" || s == "y" || s == "yes"
}

func environment() *env {
	r := strings.Split(os.Getenv("INPUT_REPO"), "/")
	ref := os.Getenv("INPUT_REF")
	e := &env{
		ref:               ref,
		owner:             r[0],
		repo:              r[1],
		branch:            branch(ref),
		sha:               os.Getenv("INPUT_SHA"),
		root:              os.Getenv("INPUT_ROOT"),
		label:             os.Getenv("INPUT_LABEL"),
		token:             os.Getenv("INPUT_TOKEN"),
		includeRE:         os.Getenv("INPUT_INCLUDE_PATTERN"),
		excludeRE:         os.Getenv("INPUT_EXCLUDE_PATTERN"),
		dryRun:            flagToBool(os.Getenv("INPUT_DRY_RUN")),
		extendedLabels:    flagToBool(os.Getenv("INPUT_EXTENDED_LABELS")),
		closeOnSameBranch: flagToBool(os.Getenv("INPUT_CLOSE_ON_SAME_BRANCH")),
		commentIssue:      flagToBool(os.Getenv("INPUT_COMMENT_ON_ISSUES")),
		assignFromBlame:   flagToBool(os.Getenv("INPUT_ASSIGN_FROM_BLAME")),
	}

	var err error

	e.minWords, err = strconv.Atoi(os.Getenv("INPUT_MIN_WORDS"))
	if err != nil {
		e.minWords = defaultMinWords
	}

	e.minChars, err = strconv.Atoi(os.Getenv("INPUT_MIN_CHARACTERS"))
	if err != nil {
		e.minChars = defaultMinChars
	}

	e.addLimit, err = strconv.Atoi(os.Getenv("INPUT_ADD_LIMIT"))
	if err != nil {
		e.addLimit = defaultAddLimit
	}

	e.closeLimit, err = strconv.Atoi(os.Getenv("INPUT_CLOSE_LIMIT"))
	if err != nil {
		e.closeLimit = defaultCloseLimit
	}

	e.concurrency, err = strconv.Atoi(os.Getenv("INPUT_CONCURRENCY"))
	if err != nil {
		e.concurrency = defaultConcurrency
	}

	e.projectColumnID, err = strconv.ParseInt(os.Getenv("INPUT_PROJECT_COLUMN_ID"), 10, 64)
	if err != nil {
		e.projectColumnID = -1
	}

	return e
}

func (e *env) debugPrint() {
	log.Printf("Repo: %v", e.repo)
	log.Printf("Ref: %v", e.ref)
	log.Printf("Sha: %v", e.sha)
	log.Printf("Root: %v", e.root)
	log.Printf("Label: %v", e.label)
	log.Printf("Column ID: %v", e.projectColumnID)
	log.Printf("Extended labels: %v", e.extendedLabels)
	log.Printf("Min words: %v", e.minWords)
	log.Printf("Min chars: %v", e.minChars)
	log.Printf("Add limit: %v", e.addLimit)
	log.Printf("Close limit: %v", e.closeLimit)
	log.Printf("Close on same branch: %v", e.closeOnSameBranch)
	log.Printf("Dry run: %v", e.dryRun)
}

func branch(ref string) string {
	parts := strings.Split(ref, "/")
	skip := map[string]bool{"refs": true, "tags": true, "heads": true, "remotes": true}
	i := 0

	for i = 0; i < len(parts); i++ {
		if _, ok := skip[parts[i]]; !ok {
			break
		}
	}

	return strings.Join(parts[i:], "/")
}

func (s *service) fetchGithubIssues() ([]*github.Issue, error) {
	var allIssues []*github.Issue

	opt := &github.IssueListByRepoOptions{
		Labels:      []string{s.env.label},
		State:       "all",
		ListOptions: github.ListOptions{PerPage: defaultIssuesPerPage},
	}

	for {
		issues, resp, err := s.client.Issues.ListByRepo(s.ctx, s.env.owner, s.env.repo, opt)
		if err != nil {
			return nil, err
		}

		allIssues = append(allIssues, issues...)

		if resp.NextPage == 0 {
			break
		}

		opt.Page = resp.NextPage
	}
	log.Printf("Fetched github todo issues. count=%v label=%v", len(allIssues), s.env.label)

	return allIssues, nil
}

func escapePath(path string) string {
	parts := strings.Split(path, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}

	return strings.Join(parts, "/")
}

func (s *service) createFileLink(c *tdglib.ToDoComment) string {
	start := c.Line - contextLinesUp
	if start < 0 {
		start = 0
	}

	end := c.Line + contextLinesDown

	root := s.env.root
	root = strings.TrimPrefix(root, ".")
	root = strings.TrimPrefix(root, "/")
	root = strings.TrimSuffix(root, "/")

	filepath := c.File
	if root != "." {
		filepath = fmt.Sprintf("%v/%v", root, c.File)
	}

	safeFilepath := escapePath(filepath)

	// https://github.com/{repo}/blob/{sha}/{file}#L{startLines}-L{endLine}
	return fmt.Sprintf("https://github.com/%s/%s/blob/%s/%s#L%v-L%v",
		s.env.owner, s.env.repo, s.env.sha, safeFilepath, start, end)
}

func (s *service) labels(c *tdglib.ToDoComment) []string {
	labels := []string{s.env.label}
	if s.env.extendedLabels {
		labels = append(labels, labelBranchPrefix+s.env.branch)
		labels = append(labels, labelTypePrefix+strings.ToLower(c.Type))

		if len(c.Category) > 0 {
			labels = append(labels, labelAreaPrefix+c.Category)
		}

		if c.Estimate > minEstimate {
			minutes := math.Round(c.Estimate * hourMinutes)
			estimate := ""

			if minutes >= hourMinutes {
				// -1 means use the smallest number of digits
				estimate = strconv.FormatFloat(c.Estimate, 'f', -1, 32) + "h"
			} else {
				estimate = fmt.Sprintf("%vm", minutes)
			}

			labels = append(labels, fmt.Sprintf("estimate: %v", estimate))
		}
	}

	return labels
}

func (s *service) createProjectCard(issue *github.Issue) {
	opts := &github.ProjectCardOptions{
		ContentType: "Issue",
		ContentID:   issue.GetID(),
	}
	card, _, err := s.client.Projects.CreateProjectCard(s.ctx, s.env.projectColumnID, opts)

	if err != nil {
		log.Printf("Failed to create a project card. err=%v", err)
		return
	}

	log.Printf("Created a project card. issue=%v card=%v", issue.GetID(), card.GetID())
}

func (s *service) openNewIssues(issueMap map[string]*github.Issue, comments []*tdglib.ToDoComment) {
	defer s.wg.Done()
	count := 0

	for _, c := range comments {
		_, ok := issueMap[c.Title]
		if !ok {
			body := c.Body + "\n\n"
			if c.Issue > 0 {
				body += fmt.Sprintf("Parent issue: #%v\n", c.Issue)
			}

			if len(c.Author) > 0 {
				body += fmt.Sprintf("Author: @%s\n", c.Author)
			}

			body += fmt.Sprintf("Line: %v\n%s", c.Line, s.createFileLink(c))

			log.Printf("About to create an issue. title=%v body=%v", c.Title, body)

			if s.env.dryRun {
				log.Printf("Dry run mode.")
				continue
			}

			labels := s.labels(c)
			var assignees []string
			if s.env.assignFromBlame {
				if len(c.CommitHash) > 0 {
					commit, _, err := s.client.Git.GetCommit(s.ctx, s.env.owner, s.env.repo, c.CommitHash)
					if err != nil {
						log.Printf("Error while getting commit from commit hash. err=%v", err)
					} else {
						assignees = append(assignees, *commit.Author.Login)
					}
				}
			}
			req := &github.IssueRequest{
				Title:     &c.Title,
				Body:      &body,
				Labels:    &labels,
				Assignees: &assignees,
			}

			issue, _, err := s.client.Issues.Create(s.ctx, s.env.owner, s.env.repo, req)
			if err != nil {
				log.Printf("Error while creating an issue. err=%v", err)
				continue
			}

			log.Printf("Created an issue. title=%v issue=%v", c.Title, issue.GetID())

			if s.env.projectColumnID != -1 {
				s.createProjectCard(issue)
			}

			count++
			if s.env.addLimit > 0 && count >= s.env.addLimit {
				log.Printf("Exceeded limit of issues to create. limit=%v", s.env.addLimit)
				break
			}
		}
	}

	log.Printf("Created new issues. count=%v", count)
}

func (s *service) canCloseIssue(issue *github.Issue) bool {
	if !s.env.closeOnSameBranch {
		return true
	}

	labels := issue.Labels
	anyBranch := false

	log.Printf("Retrieved issue labels. issue=%v labels=%v", issue.GetID(), len(labels))

	for _, l := range labels {
		name := l.GetName()
		if strings.HasPrefix(name, labelBranchPrefix) {
			anyBranch = true
			branch := strings.TrimPrefix(name, labelBranchPrefix)

			if branch == s.env.branch {
				return true
			}
		}
	}

	log.Printf("Checking issue labels. issue=%v any_branch=%v", issue.GetID(), anyBranch)

	// if the issues does not have a branch tag, assume we can close it
	return !anyBranch
}

func (s *service) commentIssue(body string, i *github.Issue) {
	comment := &github.IssueComment{
		Body: &body,
	}
	_, _, err := s.client.Issues.CreateComment(s.ctx, s.env.owner, s.env.repo, i.GetNumber(), comment)
	if err != nil {
		log.Printf("Error while adding a comment. issue=%v err=%v", i.ID, err)
		return
	}

	log.Printf("Added a comment to the issue. issue=%v", i.ID)
}

func (s *service) closeMissingIssues(issueMap map[string]*github.Issue, comments []*tdglib.ToDoComment) {
	defer s.wg.Done()

	count := 0
	commentsMap := make(map[string]*tdglib.ToDoComment)
	closed := "closed"

	for _, c := range comments {
		commentsMap[c.Title] = c
	}

	for _, i := range issueMap {
		if _, ok := commentsMap[i.GetTitle()]; ok {
			continue
		}

		log.Printf("About to close an issue. issue=%v title=%v", i.GetID(), i.GetTitle())

		if s.env.dryRun {
			log.Printf("Dry run mode")
			continue
		}

		if i.GetState() == closed {
			log.Printf("Issue is already closed. issue=%v", i.GetNumber())
			continue
		}

		canClose := s.canCloseIssue(i)
		if !canClose {
			log.Printf("Cannot close the issue. issue=%v", i.GetID())
			continue
		}

		if s.env.commentIssue {
			s.commentIssue(fmt.Sprintf("Closed in commit %v", s.env.sha), i)
		}

		req := &github.IssueRequest{
			State: &closed,
		}
		_, _, err := s.client.Issues.Edit(s.ctx, s.env.owner, s.env.repo, i.GetNumber(), req)

		if err != nil {
			log.Printf("Error while closing an issue. issue=%v err=%v", i.GetID(), err)
			continue
		}

		log.Printf("Closed an issue. issue=%v", i.GetID())

		count++
		if s.env.closeLimit > 0 && count >= s.env.closeLimit {
			log.Printf("Exceeded limit of issues to close. limit=%v", s.env.closeLimit)
			break
		}
	}

	log.Printf("Closed issues. count=%v", count)
}

func main() {
	log.SetOutput(os.Stdout)

	env := environment()
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: env.token},
	)
	tc := oauth2.NewClient(ctx, ts)

	svc := &service{
		ctx:    ctx,
		client: github.NewClient(tc),
		env:    env,
	}

	env.debugPrint()

	issues, err := svc.fetchGithubIssues()
	if err != nil {
		log.Panic(err)
	}

	includePatterns := make([]string, 0)
	if len(env.includeRE) > 0 {
		includePatterns = append(includePatterns, env.includeRE)
	}

	excludePatterns := make([]string, 0)
	if len(env.excludeRE) > 0 {
		excludePatterns = append(excludePatterns, env.excludeRE)
	}

	td := tdglib.NewToDoGenerator(env.sourceRoot(),
		includePatterns,
		excludePatterns,
		env.assignFromBlame,
		env.minWords,
		env.minChars,
		env.concurrency)

	if env.assignFromBlame {
		command := "git"
		args := []string{"config", "--global", "--add", "safe.directory", env.sourceRoot()}

		cmd := exec.Command(command, args...)
		out, err := cmd.Output()
		if out != nil {
			fmt.Println(out)
		}
		if err != nil {
			fmt.Printf("Error running git command: %v\n", err)
			return
		}
	}

	comments, err := td.Generate()
	if err != nil {
		log.Panic(err)
	}

	log.Printf("Extracted TODO comments. count=%v", len(comments))

	issueMap := make(map[string]*github.Issue)
	for _, i := range issues {
		issueMap[i.GetTitle()] = i
	}

	svc.wg.Add(1)
	go svc.closeMissingIssues(issueMap, comments)

	svc.wg.Add(1)
	go svc.openNewIssues(issueMap, comments)

	log.Printf("Waiting for issues management to finish")
	svc.wg.Wait()

	fmt.Println(fmt.Sprintf(`::set-output name=scannedIssues::%s`, "1"))
}

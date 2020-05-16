package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/google/go-github/v31/github"
	"github.com/ribtoks/tdg/pkg/tdglib"
	"golang.org/x/oauth2"
)

const (
	defaultMinWords      = 3
	defaultMinChars      = 30
	defaultAddLimit      = 0
	defaultCloseLimit    = 0
	defaultIssuesPerPage = 100
	contextLinesUp       = 3
	contextLinesDown     = 7
	ghRoot               = "/github/workspace"
	minEstimate          = 0.01
	hourMinutes          = 60
	labelBranchPrefix    = "branch: "
	labelTypePrefix      = "type: "
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
	closeOnSameBranch bool
	extendedLabels    bool
	dryRun            bool
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
		owner:          r[0],
		repo:           r[1],
		ref:            ref,
		branch:         branch(ref),
		label:          os.Getenv("INPUT_LABEL"),
		token:          os.Getenv("INPUT_TOKEN"),
		sha:            os.Getenv("INPUT_SHA"),
		includeRE:      os.Getenv("INPUT_INCLUDE_PATTERN"),
		excludeRE:      os.Getenv("INPUT_EXCLUDE_PATTERN"),
		root:           os.Getenv("INPUT_ROOT"),
		dryRun:         flagToBool(os.Getenv("INPUT_DRY_RUN")),
		extendedLabels: flagToBool(os.Getenv("INPUT_EXTENDED_LABELS")),
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

	// https://github.com/{repo}/blob/{sha}/{file}#L{startLines}-L{endLine}
	return fmt.Sprintf("https://github.com/%s/%s/blob/%s/%s#L%v-L%v",
		s.env.owner, s.env.repo, s.env.sha, filepath, start, end)
}

func (s *service) labels(c *tdglib.ToDoComment) []string {
	labels := []string{s.env.label}
	if s.env.extendedLabels {
		labels = append(labels, labelBranchPrefix+s.env.branch)
		labels = append(labels, labelTypePrefix+strings.ToLower(c.Type))

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
			req := &github.IssueRequest{
				Title:  &c.Title,
				Body:   &body,
				Labels: &labels,
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

	opts := &github.ListOptions{}
	labels, _, err := s.client.Issues.ListLabelsByIssue(s.ctx, s.env.owner, s.env.repo, issue.GetNumber(), opts)

	if err != nil {
		log.Printf("Error while listing labels. issue=%v err=%v", issue.GetID(), err)
		return false
	}

	anyBranch := false

	for _, l := range labels {
		if strings.HasPrefix(labelBranchPrefix, l.GetName()) {
			anyBranch = true
			branch := strings.TrimPrefix(l.GetName(), labelBranchPrefix)

			if branch == s.env.branch {
				return true
			}
		}
	}

	// if the issues does not have a branch tag, assume we can close it
	return !anyBranch
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

		canClose := s.canCloseIssue(i)
		if !canClose {
			log.Printf("Cannot close the issue. issue=%v", i.GetID())
			continue
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
		env.minWords,
		env.minChars)

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

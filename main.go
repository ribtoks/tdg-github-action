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
	ghRoot               = "/github/workspace"
)

func sourceRoot(root string) string {
	if strings.HasPrefix(root, "/") {
		return ghRoot + root
	}

	return fmt.Sprintf("%s/%v", ghRoot, root)
}

func main() {
	log.SetOutput(os.Stdout)

	r := strings.Split(os.Getenv("INPUT_REPO"), "/")
	owner, repo := r[0], r[1]
	//label := os.Getenv("INPUT_LABEL")
	token := os.Getenv("INPUT_TOKEN")
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

	client := github.NewClient(tc)
	opt := &github.IssueListByRepoOptions{
		ListOptions: github.ListOptions{PerPage: defaultIssuesPerPage},
	}

	var allIssues []*github.Issue

	for {
		issues, resp, err := client.Issues.ListByRepo(ctx, owner, repo, opt)
		if err != nil {
			log.Panic(err)
		}

		allIssues = append(allIssues, issues...)

		if resp.NextPage == 0 {
			break
		}

		opt.Page = resp.NextPage
	}
	log.Printf("Fetched github issues. count=%v", len(allIssues))

	//env := tdglib.NewEnvironment(srcRoot)
	td := tdglib.NewToDoGenerator(srcRoot,
		[]string{includePattern},
		[]string{excludePattern},
		minWords,
		minChars)

	comments, err := td.Generate()
	if err != nil {
		log.Panic(err)
	}

	log.Printf("Extracted TODO comments. count=%v", len(comments))

	issueMap := make(map[string]*github.Issue)
	for _, i := range allIssues {
		issueMap[i.GetTitle()] = i
	}

	for _, c := range comments {
		_, ok := issueMap[c.Title]
		if !ok {
			log.Printf("About to create an issue. title=%v", c.Title)

			if dryRun {
				log.Printf("Dry run mode.")
				continue
			}
		}
	}
}

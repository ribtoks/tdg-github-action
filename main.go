package main

import (
	"context"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/google/go-github/v31/github"
	"github.com/ribtoks/tdg/pkg/tdglib"
	"golang.org/x/oauth2"
)

const (
	defaultMinWords = 3
	defaultMinChars = 30
)

func main() {
	log.SetOutput(os.Stdout)

	r := strings.Split(os.Getenv("INPUT_REPO"), "/")
	owner, repo := r[0], r[1]
	//label := os.Getenv("INPUT_LABEL")
	token := os.Getenv("INPUT_TOKEN")
	includePattern := os.Getenv("INPUT_INCLUDE_PATTERN")
	excludePattern := os.Getenv("INPUT_EXCLUDE_PATTERN")
	srcRoot := os.Getenv("INPUT_ROOT")
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
	opt := &github.IssueListByRepoOptions{}
	issues, _, err := client.Issues.ListByRepo(ctx, owner, repo, opt)
	if err != nil {
		log.Panic(err)
	}
	log.Printf("Fetched github issues. count=%v", len(issues))

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
	for _, i := range issues {
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

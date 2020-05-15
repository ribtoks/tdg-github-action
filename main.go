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
	r := strings.Split(os.Getenv("REPO"), "")
	owner, repo := r[0], r[1]
	//label := os.Getenv("LABEL")
	token := os.Getenv("TOKEN")
	includePattern := os.Getenv("INCLUDE_PATTERN")
	excludePattern := os.Getenv("EXCLUDE_PATTERN")
	srcRoot := os.Getenv("ROOT")
	minWords, err := strconv.Atoi(os.Getenv("MIN_WORDS"))
	dryRun := len(os.Getenv("DRY_RUN")) > 0
	if err != nil {
		minWords = defaultMinWords
	}
	minChars, err := strconv.Atoi(os.Getenv("MIN_CHARACTERS"))
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

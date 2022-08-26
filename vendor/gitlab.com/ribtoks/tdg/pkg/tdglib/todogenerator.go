package tdglib

import (
	"bufio"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/zieckey/goini"
)

const (
	estimateEpsilon = 0.01
	minTitleWords   = 2
)

var (
	commentPrefixes        = [...]string{"TODO", "FIXME", "BUG", "HACK"}
	emptyRunes             = [...]rune{}
	categoryIniKey         = "category"
	issueIniKey            = "issue"
	estimateIniKey         = "estimate"
	authorIniKey           = "author"
	errCannotParseIni      = errors.New("Cannot parse ini properties")
	errCannotParseEstimate = errors.New("Cannot parse time estimate")
)

// ToDoComment a task that is parsed from TODO comment
// estimate is in hours
type ToDoComment struct {
	Type     string  `json:"type"`
	Title    string  `json:"title"`
	Body     string  `json:"body"`
	File     string  `json:"file"`
	Line     int     `json:"line"`
	Issue    int     `json:"issue,omitempty"`
	Author   string  `json:"author,omitempty"`
	Category string  `json:"category,omitempty"`
	Estimate float64 `json:"estimate,omitempty"`
}

// ToDoGenerator is responsible for parsing code base to ToDoComments
type ToDoGenerator struct {
	root       string
	include    []*regexp.Regexp
	exclude    []*regexp.Regexp
	commentsWG sync.WaitGroup
	comments   []*ToDoComment
	minWords   int
	minChars   int
	addedMap   map[string]bool
	commentMux sync.Mutex
}

// NewToDoGenerator creates new generator for a source root
func NewToDoGenerator(root string, include []string, exclude []string, minWords, minChars int) *ToDoGenerator {
	log.Printf("Using source code root %v", root)
	log.Printf("Using %v include filters", include)
	ifilters := make([]*regexp.Regexp, 0, len(include))
	for _, f := range include {
		ifilters = append(ifilters, regexp.MustCompile(f))
	}

	log.Printf("Using %v exclude filters", exclude)
	efilters := make([]*regexp.Regexp, 0, len(exclude))
	for _, f := range exclude {
		efilters = append(efilters, regexp.MustCompile(f))
	}

	absolutePath, err := filepath.Abs(root)
	if err != nil {
		log.Printf("Error setting generator root: %v", err)

		absolutePath = root
	}

	return &ToDoGenerator{
		root:     absolutePath,
		include:  ifilters,
		exclude:  efilters,
		minWords: minWords,
		minChars: minChars,
		comments: make([]*ToDoComment, 0),
		addedMap: make(map[string]bool),
	}
}

func (td *ToDoGenerator) Root() string {
	return td.root
}

func (td *ToDoGenerator) Includes(path string) bool {
	anyMatch := false

	for _, f := range td.include {
		if f.MatchString(path) {
			anyMatch = true
			break
		}
	}

	if !anyMatch && len(td.include) > 0 {
		return false
	}

	return true
}

func (td *ToDoGenerator) Excludes(path string) bool {
	anyMatch := false

	for _, f := range td.exclude {
		if f.MatchString(path) {
			anyMatch = true
			break
		}
	}

	return anyMatch
}

// Generate is an entry point to comment generation
func (td *ToDoGenerator) Generate() ([]*ToDoComment, error) {
	matchesCount := 0
	totalFiles := 0
	err := filepath.Walk(td.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.Mode().IsRegular() {
			return nil
		}

		totalFiles++

		if !td.Includes(path) {
			return nil
		}

		if td.Excludes(path) {
			return nil
		}

		anyMatch := false
		for _, f := range td.include {
			if f.MatchString(path) {
				anyMatch = true
				break
			}
		}
		if !anyMatch && len(td.include) > 0 {
			return nil
		}

		matchesCount++
		td.commentsWG.Add(1)
		go td.parseFile(path)

		return nil
	})

	if err != nil {
		return nil, err
	}

	log.Printf("Scanned files: %v", totalFiles)
	log.Printf("Matched files: %v", matchesCount)
	td.commentsWG.Wait()
	log.Printf("Found comments: %v", len(td.comments))

	return td.comments, nil
}

func countTitleWords(s string) int {
	words := strings.Fields(s)
	count := 0

	for _, w := range words {
		if len(w) > minTitleWords {
			count++
		}
	}

	return count
}

func (td *ToDoGenerator) addComment(c *ToDoComment) {
	defer td.commentsWG.Done()

	h := md5.New()
	io.WriteString(h, c.File)
	io.WriteString(h, c.Title)
	io.WriteString(h, c.Body)
	s := hex.EncodeToString(h.Sum(nil))

	td.commentMux.Lock()
	defer td.commentMux.Unlock()

	if _, ok := td.addedMap[s]; ok {
		log.Printf("Skipping comment duplicate in %v:%v", c.File, c.Line)
		return
	}

	if countTitleWords(c.Title) >= td.minWords || len(c.Title) >= td.minChars {
		td.addedMap[s] = true
		td.comments = append(td.comments, c)
	} else {
		log.Printf("Ignoring comment in %v:%v", c.File, c.Line)
	}
}

func isCommentRune(r rune) bool {
	return r == '/' ||
		r == '#' ||
		r == '%' ||
		r == ';' ||
		r == '*'
}

// try to parse comment body from commented line
func parseComment(line string) []rune {
	runes := []rune(line)
	i := 0
	size := len(runes)
	// skip prefix whitespace
	for i < size && unicode.IsSpace(runes[i]) {
		i++
	}

	hasComment := false
	// skip comment symbols themselves
	for i < size && isCommentRune(runes[i]) {
		i++
		hasComment = true
	}

	if !hasComment {
		return nil
	}
	// and skip space again
	for i < size && unicode.IsSpace(runes[i]) {
		i++
	}

	j := size - 1
	// skip suffix whitespace
	for j > i && unicode.IsSpace(runes[j]) {
		j--
	}

	// empty comment
	if i >= size || j < 0 || i >= j {
		return emptyRunes[:]
	}

	return runes[i : j+1]
}

func startsWith(s, pr []rune) bool {
	// do not check length (it's checked above)
	for i, p := range pr {
		if unicode.ToUpper(s[i]) != p {
			return false
		}
	}

	return true
}

func parseToDoTitle(line []rune) (ctype, title, author []rune) {
	if len(line) == 0 {
		return nil, nil, nil
	}

	size := len(line)

	for _, pr := range commentPrefixes {
		prlen := len(pr)
		if size > prlen && startsWith(line, []rune(pr)) {
			i := prlen
			if unicode.IsLetter(line[i]) {
				continue
			}

			ctype = []rune(pr)[:prlen]

			if line[i] == '(' {
				for i < size && line[i] != ')' {
					i++
				}

				author = line[prlen+1 : i]
			}

			for i < size &&
				!unicode.IsSpace(line[i]) &&
				line[i] != ':' {
				i++
			}

			for i < size && (unicode.IsSpace(line[i]) || line[i] == ':') {
				i++
			}

			if i < size {
				title = line[i:]
				return
			}
		}
	}

	return nil, nil, nil
}

// parseEstimate parses human-readible hours or minutes
// estimate to float64 in hours
func parseEstimate(estimate string) (float64, error) {
	if len(estimate) == 0 {
		return 0, errCannotParseEstimate
	}
	var s string
	last := rune(estimate[len(estimate)-1])
	if unicode.IsLetter(last) && last != 'm' && last != 'h' {
		return 0, errCannotParseEstimate
	}

	if unicode.IsLetter(last) {
		s = estimate[:len(estimate)-1]
	} else {
		s = estimate
	}

	if f, err := strconv.ParseFloat(s, 64); err == nil {
		if last == 'm' {
			return f / 60.0, nil
		}
		return f, nil
	}
	return 0, errCannotParseEstimate
}

func (t *ToDoComment) parseIniProperties(line string) error {
	if !strings.Contains(line, "=") {
		return errCannotParseIni
	}
	ini := goini.New()
	err := ini.Parse([]byte(line), " ", "=")
	if err != nil {
		return err
	}
	if v, ok := ini.Get(categoryIniKey); ok {
		t.Category = v
	}
	if v, ok := ini.Get(authorIniKey); ok {
		if len(t.Author) == 0 {
			t.Author = v
		}
	}
	if v, ok := ini.Get(issueIniKey); ok {
		if i, err := strconv.Atoi(v); err == nil {
			t.Issue = i
		}
	}
	if v, ok := ini.Get(estimateIniKey); ok {
		if f, err := parseEstimate(v); err == nil {
			t.Estimate = f
		}
	}

	if len(t.Category) == 0 &&
		t.Issue == 0 &&
		t.Estimate < estimateEpsilon {
		return errCannotParseIni
	}
	return nil
}

// NewComment creates new task from parsed comment lines
func NewComment(path string, lineNumber int, ctype, author string, body []string) *ToDoComment {
	if body == nil || len(body) == 0 {
		return nil
	}

	t := &ToDoComment{
		Type:   ctype,
		Title:  body[0],
		File:   path,
		Line:   lineNumber,
		Author: author,
	}

	if len(body) > 1 {
		var commentBody string
		if err := t.parseIniProperties(body[1]); err == nil {
			commentBody = strings.Join(body[2:], "\n")
		} else {
			commentBody = strings.Join(body[1:], "\n")
		}
		t.Body = strings.TrimSpace(commentBody)
	}

	return t
}

func (td *ToDoGenerator) accountComment(path string, lineNumber int, ctype, author string, body []string) {

	relativePath, err := filepath.Rel(td.root, path)
	if err != nil {
		relativePath = path
	}
	c := NewComment(relativePath, lineNumber, ctype, author, body)
	if c != nil {
		td.commentsWG.Add(1)
		go td.addComment(c)
	}
}

func (td *ToDoGenerator) parseFile(path string) {
	defer td.commentsWG.Done()
	f, err := os.Open(path)
	if err != nil {
		log.Print(err)
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	var todo []string
	var lastType string
	var lastAuthor string
	var lastStart int
	lineNumber := 0
	for scanner.Scan() {
		line := scanner.Text()
		lineNumber++
		if c := parseComment(line); c != nil {
			// current comment is new TODO-like commment
			if ctype, title, author := parseToDoTitle(c); title != nil {
				// do we need to finalize previous
				if lastType != "" {
					td.accountComment(path, lastStart+1, lastType, lastAuthor, todo)
				}
				// construct new one
				lastAuthor = string(author)
				lastType = string(ctype)
				lastStart = lineNumber - 1
				todo = make([]string, 0)
				todo = append(todo, string(title))
			} else if lastType != "" {
				// continue consecutive comment line
				todo = append(todo, string(c))
			}
		} else {
			// not a comment anymore: finalize
			if lastType != "" {
				td.accountComment(path, lastStart+1, lastType, lastAuthor, todo)
				lastType = ""
			}
		}
	}
	// detect todo item at the end of the file
	if lastType != "" {
		td.accountComment(path, lastStart+1, lastType, lastAuthor, todo)
	}
}

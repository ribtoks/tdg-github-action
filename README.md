# Turn your TODO comments into GitHub Issues

[![Build](https://github.com/ribtoks/tdg-github-action/workflows/Build/badge.svg)](https://github.com/ribtoks/tdg-github-action/actions)
[![Integration Test](https://github.com/ribtoks/tdg-github-action/workflows/Integration%20Test/badge.svg)](https://github.com/ribtoks/tdg-github-action/actions)

GitHub Action that will manage issues based on `TODO`/`BUG`/`FIXME`/`HACK` comments in the source code. Source code is parsed using [tdg](https://gitlab.com/ribtoks/tdg) which supports comments for almost all existing languages.

When a new todo comment is added, a new issue is created. When this comment is removed on the branch it was added, the corresponding issue is closed. Each issue is added with a special label so you can build more automation on top of it.

## Screenshot

![TDG result](screenshot.png "Example of created issue")

## Usage

Create a workflow file in your `.github/workflows/` directory with the following contents:

### Basic example

```yaml
name: TODO workflow
on: [push, pull_request]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
    - uses: actions/checkout@master
    - name: Run tdg-github-action
      uses: ribtoks/tdg-github-action@master
      with:
        TOKEN: ${{ secrets.GITHUB_TOKEN }}
        REPO: ${{ github.repository }}
        SHA: ${{ github.sha }}
        REF: ${{ github.ref }}
```

> **NOTE:** Please note that currently GitHub has 5000 requests per hour limit so if you are running it on a fresh repository and you have lots of todos in comments, you may hit this limit.

You can use this action together with [parent issue updater](https://github.com/ribtoks/parent-issue-update) in order to automatically keep track of child TODO items in parent issues. For that you need to use `issue=123` extension in the TODO comment - see example below.

### Inputs

| Input | Description |
|---|---|
| `REPO`  | Repository name in the format of `owner/repo` (required)   |
| `ISSUE_REPO`  | Repository to create issues in (if empty, `REPO` is used)   |
| `TOKEN`  | Github [token](#security-token) used to create or close issues (required)  |
| `REF`  | Git ref: branch or pull request (required)|
| `SHA`  | SHA-1 value of the commit (required) |
| `ROOT`  | Source code root (defaults to `.`) |
| `LABEL`  | Label to add to the new issues (defaults to `todo comment`) |
| `EXTENDED_LABELS`  | Add additional labels to mark branch, code language, issue type and estimate |
| `CLOSE_ON_SAME_BRANCH`  | Close issues only if they are missing from the same branch as they were created on (by default) |
| `INCLUDE_PATTERN`  | Regex to include source code files (includes all by default) |
| `EXCLUDE_PATTERN`  | Regex to exclude source code files (excludes none by default) |
| `MIN_WORDS`  | Minimum number of words in the comment to become an issue (defaults to `3`) |
| `MIN_CHARACTERS`  | Minimum number of characters in the comment to become an issue (defaults to `30`) |
| `DRY_RUN`  | Do not open or close real issues (used for debugging) |
| `ADD_LIMIT`  | Upper cap on the number of issues to create (defaults to `0` - unlimited) |
| `CLOSE_LIMIT`  | Upper cap on the number of issues to close (defaults to `0` - unlimited) |
| `COMMENT_ON_ISSUES` | Leave a comment in which commit the issue was closed (defaults to `0` - do not comment) |
| `CONCURRENCY` | How many files to process in parallel (defaults to `128`) |
| `ASSIGN_FROM_BLAME` | Get the author of the comment via git API from the commit hash of the comment and assign to the issue created (defaults to `0` - do not use) |

> **NOTE:** Keep in mind that you have to escape slashes in regex patterns when putting them to yaml

Flag values like `CLOSE_ON_SAME_BRANCH` or `DRY_RUN` use values `1`/`true`/`y` as ON switch.


In case you are disabling `EXTENDED_LABELS`, then `CLOSE_ON_SAME_BRANCH` logic will be broken since there will be no knowledge on which branch the issue was created (for new issues), effectively making it disabled.

### Security (token)

You can of course use a private token or, if you want to use a default `GITHUB_TOKEN`, available for CI, you need to add read and write permissions in the _Repository -> Settings -> Actions -> General -> Workflow permissions_ select `"Read and write permissions"`.

### Outputs

| Output                                             | Description                                        |
|------------------------------------------------------|-----------------------------------------------|
| `scannedIssues`  | Equals to `1` if completed successfully    |

## Examples

### Workflow

```yaml
name: TDG
on: [push, pull_request]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
    - uses: actions/checkout@master
    - name: Run tdg-github-action
      uses: ribtoks/tdg-github-action@master
      with:
        TOKEN: ${{ secrets.GITHUB_TOKEN }}
        REPO: ${{ github.repository }}
        SHA: ${{ github.sha }}
        REF: ${{ github.ref }}
        LABEL: "my label"
        MIN_WORDS: 3
        MIN_CHARACTERS: 40
        ADD_LIMIT: 1
        CLOSE_LIMIT: 1
        ROOT: "src"
        INCLUDE_PATTERN: "\\.(cpp|h)$"
```

Note escaped regex.

If you want to only process TODO comments from `master` branch, modify the workflow `on` section like this:

```yaml
on:
  # only for the master branch
  push:
    branches:
    - master
```

### TODO comments

Comments are parsed using [tdg](https://gitlab.com/ribtoks/tdg). Supported comments: `//`, `#`, `%`, `;`, `*`.

Example of the comment (everything but the first line is optional):

    // TODO: This is title of the issue to create
    // category=SomeCategory issue=123 estimate=30m author=alias
    // This is a multiline description of the issue
    // that will be in the "Body" property of the comment

Note that second line has some optional "extensions" added as metadata to the issue by [tdg](https://gitlab.com/ribtoks/tdg). Some are turned into labels and also used by [parent issue updater](https://github.com/ribtoks/parent-issue-update).

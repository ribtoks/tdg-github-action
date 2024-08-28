.PHONY: clean build deploy

GIT_COMMIT ?= $(shell git rev-list -1 HEAD)

build:
	env GOFLAGS="-mod=vendor" CGO_ENABLED=0 go build -ldflags="-s -w -X main.GitCommit=$(GIT_COMMIT)" -o bin/github-tdg *.go

test:
	env GOFLAGS="-mod=vendor" CGO_ENABLED=0 go test ./...

vendors:
	go mod tidy
	go mod vendor

clean:
	rm -rf ./bin Gopkg.lock

deploy:
	echo "TODO: deploy"

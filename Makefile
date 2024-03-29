.PHONY: clean build deploy

GIT_COMMIT ?= $(shell git rev-list -1 HEAD)

build:
	export GOFLAGS="-mod=vendor"
	go build

test:
	go test ./...

vendors:
	go mod tidy
	go mod vendor

clean:
	rm -rf ./bin Gopkg.lock

deploy:
	echo "TODO: deploy"

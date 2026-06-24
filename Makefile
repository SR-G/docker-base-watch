SOURCEDIR=.
SOURCES := $(shell find $(SOURCEDIR) -name '*.go')

BINARY=docker-base-watch
PWD := $(shell pwd)

GOPATH := $(shell echo "${HOME}")/go
BUILD_DIR := $(shell echo "${PWD}")/bin
DISTRIBUTION_DIR := $(shell echo "${PWD}")/distribution

VERSION=1.1.0
VERSION_LABEL=RELEASE
PACKAGE=docker-base-watch
BUILD_TIME=$(shell date "+%FT%T%z")

LDFLAGS=-trimpath -ldflags "-s -w -X 'main.BuildCommit=`git rev-parse HEAD`' -X 'main.BuildVersion=${VERSION}' -X 'main.BuildVersionLabel=${VERSION_LABEL}' -X 'main.BuildCompilationTimestamp=${BUILD_TIME}'"
LINUX_EXTRA_FLAGS=-a -tags netgo -installsuffix netgo
DEBUG_FLAGS=-gcflags=all="-N -l"
CGO_ENABLED=CGO_ENABLED=0

.PHONY: install clean deploy run 

.ONESHELL: # Applies to every targets in the file!

clean: ## [DEV] Clean everything
	rm -rf ${BUILD_DIR} ${DISTRIBUTION_DIR}

build: clean ## [DEV] Quick compile the main (linux) binary
	mkdir -p ${BUILD_DIR} 
	${CGO_ENABLED} GOOS=linux GOARCH=amd64 GOOS=linux go build ${LDFLAGS} -o ${BUILD_DIR}/${BINARY} ${PACKAGE} 

mod: ## [DEV] Update go modules
	go mod tidy

run: ## [DEV] Run the program with DEV configuration
	${BUILD_DIR}/${BINARY} --threads 2 --images "alpine postgres"

format: ## [DEV] Format code
	gofmt -w .

version: build ## [DEV] Extract the version (in go resources way) from binary
	go version -m bin/${BINARY}

nm: ## [DEV] Analyze symbols in binary (need to remove LDFLAGS -s -w)
	go tool nm bin/${BINARY}

install: clean ## [RELEASE] Build all target binaries on all expected platforms
	${CGO_ENABLED} GOARCH=amd64 GOOS=windows go build ${LDFLAGS} -o ${BUILD_DIR}/windows_amd64/${BINARY}.exe                                 ${PACKAGE}
	${CGO_ENABLED} GOARCH=amd64 GOOS=linux   go build ${LDFLAGS} -o ${BUILD_DIR}/linux_amd64/${BINARY} ${LINUX_EXTRA_FLAGS} ${PACKAGE}
	${CGO_ENABLED} GOARCH=arm64 GOOS=linux   go build ${LDFLAGS} -o ${BUILD_DIR}/linux_arm64/${BINARY} ${LINUX_EXTRA_FLAGS} ${PACKAGE}

docker-build: clean build ## [RELEASE] Build docker image with multi-stage Dockerfile
	docker build -t docker-base-watch .

docker-run: ## [DEV] Launch docker image (for local test purpose)
	docker run --rm --name "docker-base-watch" -v /var/run/docker.sock:/var/run/docker.sock:ro docker-base-watch --threads 2 --images "alpine postgres"

distribution: install ## [RELEASE] Build the target archive with the expected binaries
	mkdir -p ${DISTRIBUTION_DIR}
	cp ${GOPATH}/bin/${BINARY}                   ${DISTRIBUTION_DIR}/${BINARY}-linux-amd64
	cp ${GOPATH}/bin/linux_arm64/${BINARY}       ${DISTRIBUTION_DIR}/${BINARY}-linux-arm64
	cp ${GOPATH}/bin/windows_amd64/${BINARY}.exe ${DISTRIBUTION_DIR}/${BINARY}-windows-amd64.exe

help:    ## [HELP] Display commands defined in this makefile
	@sed \
		-e '/^[a-zA-Z0-9_\-]*:.*##/!d' \
		-e 's/:.*##\s*/:/' \
		-e 's/^\(.\+\):\(.*\)/$(shell tput setaf 6)\1$(shell tput sgr0):\2/' \
		$(MAKEFILE_LIST) | sort | column -c2 -t -s :

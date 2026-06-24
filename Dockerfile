FROM        golang:1.26-alpine AS builder

WORKDIR     /app

# Trick to have a docker image layer (at build stage) reusable accross builds
# (note : HOME/GOPATH have to be in line with what is in Makefile)
ENV         HOME=/app GOPATH=/app/go
COPY        go.mod go.sum ./
RUN         apk --update add git make && go mod download

# Build the project through regular Makefile
COPY        . .
RUN         make build

FROM        gcr.io/distroless/static-debian12

ENTRYPOINT  ["/app/docker-base-watch"]
WORKDIR     /app

COPY        --from=builder /app/bin/docker-base-watch /app/docker-base-watch
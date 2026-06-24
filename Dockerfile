FROM        gcr.io/distroless/static-debian12

ENTRYPOINT  ["/app/docker-base-watch"]
WORKDIR     /app

COPY        distribution/docker-base-watch-linux-amd64 /app/docker-base-watch
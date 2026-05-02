# docker-base-watch

`base-watch` is a small GOLANG CLI tool that compares a locally available Docker image with the latest image found in the image registry. It inspects the local Docker daemon, retrieves the image digest, and compares it with the digest from the remote registry to determine whether the image is up to date.

## Features

- Reads a Docker image name from command line flags
- Assumes `:latest` when no tag is provided
- Compares local image digest with remote image digest
- Supports verbose logging
- Supports version display

## Requirements

- Docker daemon accessible from the local environment

## Usage

The program accepts the image name with the `-i` or `--image` flag.

```bash
docker-base-watch -i nginx
```

If the image tag is omitted, `docker-base-watch` will automatically use `:latest`.

```bash
docker-base-watch -i ubuntu
```

### Flags

- `-i`, `--image`: Docker image name to check (required)
- `-v`, `--verbose`: Enable verbose output
- `--version`: Display program version

### Examples

Check a Docker image and compare digests:

```bash
docker-base-watch -i nginx:latest
```

Use verbose mode for more detailed logging:

```bash
docker-base-watch -i alpine -v
```

Display the program version:

```bash
docker-base-watch --version
```

### Example `--help` Output

```bash
docker-base-watch --help
```

Example output:

```bash
docker-base-watch

  Flags: 
    -h --help      Displays help with available flag, subcommand, and positional value parameters.
    -i --image     Docker image name to check
    -v --verbose   Enable verbose output
       --version   Display program version
    -q --quiet     Enable quiet output (final result to be retrieved only through exit value, 
                   i.e., echo $?)
```

### Shell example

To be used in some CI/CD commands - a simplified example could be : 

```bash
BASE_IMAGE=alpine
base-watch -q -i "$BASE_IMAGE"
UPDATE_AVAILABLE=$?
if [[ "$UPDATE_AVAILABLE" -eq 0 ]] ; then
  echo "No newer image for base [$ROOT], base image [$BASE_IMAGE]"
else
  # do something useful, like rebuilding other docker images, etc.
fi
```

### Notes

- `docker-base-watch` must be run in an environment where Docker is installed and the Docker daemon is reachable.
- If the local image is not present, the tool will report an error from the Docker daemon.
- The tool compares image digests to detect whether the local image and remote image refer to the same version.

## Installation

Build the program from source inside the main directory :

```bash
cd ~/docker-base-watch/
go build -o docker-base-watch
```


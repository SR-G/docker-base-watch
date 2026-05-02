package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/integrii/flaggy"
	"github.com/juju/loggo"
	"github.com/moby/moby/client"
)

var logger = loggo.GetLogger("base-watch")

const (
	PROGRAM_VERSION              = "1.0.0"
	EXIT_VALUE_SAME_VERSION      = 0
	EXIT_VALUE_DIFFERENT_VERSION = 1 // 2 is reserved
	EXIT_VALUE_INVALID_ARGUMENTS = 3
	EXIT_VALUE_INTERNAL_ERROR    = 4
	EXIT_VALUE_NOTHING_DONE      = 5
)

type BaseWatch struct {
	ImageName string

	client *client.Client
}

func NewBaseWatch(imageName string) *BaseWatch {
	return &BaseWatch{
		ImageName: imageName,
	}
}

func (bw *BaseWatch) Init() error {
	cli, err := client.New(client.WithHostFromEnv(), client.WithAPIVersionFromEnv())
	if err != nil {
		return fmt.Errorf("failed to create Docker client: %v", err)
	}
	bw.client = cli
	return nil
}

func (bw *BaseWatch) CheckImageVersion() (string, string, error) {
	ctx := context.Background()
	if err := bw.Init(); err != nil {
		return "", "", err
	}
	defer bw.client.Close()

	// Get local image digest
	localInspect, err := bw.client.ImageInspect(ctx, bw.ImageName)
	if err != nil {
		return "", "", fmt.Errorf("failed to inspect local image: %v", err)
	}

	var localDigest string
	if len(localInspect.RepoDigests) > 0 {
		for _, digest := range localInspect.RepoDigests {
			if strings.HasPrefix(digest, bw.ImageName[:strings.LastIndex(bw.ImageName, ":")]) {
				localDigest = strings.Split(digest, "@")[1]
				break
			}
		}
	} else {
		localDigest = localInspect.ID
	}

	// Get remote digest
	dist, err := bw.client.DistributionInspect(ctx, bw.ImageName, client.DistributionInspectOptions{})
	if err != nil {
		return "", "", fmt.Errorf("failed to inspect remote image: %v", err)
	}

	remoteDigest := dist.Descriptor.Digest.String()

	return localDigest, remoteDigest, nil
}

func addImageTagIfNeeded(imageName string) string {
	// If no tag specified, assume latest
	if !strings.Contains(imageName, ":") {
		imageName += ":latest"
	}
	return imageName
}

func main() {
	// Configure default log level to INFO
	loggo.ConfigureLoggers("<root>=INFO")

	var parameterImageName string
	var parameterVerbose bool
	var parameterVersion bool
	var parameterQuiet bool

	flaggy.String(&parameterImageName, "i", "image", "Docker image name to check")
	flaggy.Bool(&parameterVerbose, "v", "verbose", "Enable verbose output")
	flaggy.Bool(&parameterVersion, "", "version", "Display program version")
	flaggy.Bool(&parameterQuiet, "q", "quiet", "Enable quiet output (final result to be retrieved only through exit value, i.e., echo $?)")
	flaggy.DefaultParser.ShowVersionWithVersionFlag = false
	flaggy.Parse()

	if parameterVersion {
		logger.Infof("base-watch version: %s", PROGRAM_VERSION)
		os.Exit(EXIT_VALUE_NOTHING_DONE)
	}

	if parameterImageName == "" {
		logger.Errorf("image name is required")
		flaggy.DefaultParser.ShowHelp()
		os.Exit(EXIT_VALUE_INVALID_ARGUMENTS)
	} else {
		parameterImageName = addImageTagIfNeeded(parameterImageName)
	}

	if parameterVerbose {
		loggo.ConfigureLoggers("<root>=DEBUG")
		logger.Debugf("Verbose mode enabled")
		logger.Debugf("Now checking image: %s", parameterImageName)
	}

	baseWatch := NewBaseWatch(parameterImageName)
	localDigest, remoteDigest, err := baseWatch.CheckImageVersion()
	if err != nil {
		logger.Errorf("Error checking image version: %v", err)
		os.Exit(EXIT_VALUE_INTERNAL_ERROR)
	}

	logger.Debugf("Analyzed image  : %s", parameterImageName)
	logger.Debugf("- Local digest  : %s", localDigest)
	logger.Debugf("- Remote digest : %s", remoteDigest)

	logger.Debugf("Digest comparison completed")

	if localDigest == remoteDigest {
		logger.Debugf("Images are the same version.")
		if !parameterQuiet {
			fmt.Println("NO_UPDATE_AVAILABLE")
		}
		os.Exit(EXIT_VALUE_SAME_VERSION)
	} else {
		logger.Debugf("Images are different versions.")
		if !parameterQuiet {
			fmt.Println("UPDATE_AVAILABLE")
		}
		os.Exit(EXIT_VALUE_DIFFERENT_VERSION)
	}

}

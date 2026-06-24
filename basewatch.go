package main

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/docker/go-sdk/config"
	"github.com/moby/moby/api/pkg/authconfig"
	"github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/client"
	"github.com/samber/lo"
)

// Structure allowing to, through the Docker SDK, analyze images
// Embeds a docker client (to be initialized through the Init() method
type BaseWatch struct {
	client              *client.Client
	ctx                 context.Context
	dockerConfiguration *config.Config
	authConfigs         map[string]registry.AuthConfig

	Progress  func(result AnalyzeResult)
	NbThreads int
}

// Structure containing the informations related to one analyse
type AnalyzeResult struct {
	ImageNameSource           string
	ImageNameAnalyzed         string
	DigestLocal               string
	DigestRemote              string
	Identical                 bool
	ErrorDuringAnalysis       bool
	ErrorImageNotFoundInLocal bool
	Error                     error
}

func (ar *AnalyzeResult) Status() string {
	if ar.ErrorImageNotFoundInLocal {
		return RESULT_STATUS_ERROR_NOT_FOUND_IN_LOCAL
	}
	if ar.ErrorDuringAnalysis {
		return RESULT_STATUS_ERROR
	}
	if ar.Identical {
		return RESULT_STATUS_NO_UPDATE_AVAILABLE
	} else {
		return RESULT_STATUS_UPDATE_AVAILABLE
	}
}

// (if needed) array of results
type AnalyzeResults []*AnalyzeResult

func (ar *AnalyzeResults) NumberOfAnalysisInError() int {
	nb := 0
	for _, result := range *ar {
		if result.ErrorDuringAnalysis {
			nb++
		}
	}

	return nb
}

func (ar *AnalyzeResults) NumberOfDifferentImages() int {
	nb := 0
	for _, result := range *ar {
		if !result.Identical {
			nb++
		}
	}

	return nb
}

func NewBaseWatch() *BaseWatch {
	return &BaseWatch{}
}

func addImageTagIfNeeded(s string) string {
	// If no tag specified, assume latest
	imageName := strings.TrimSpace(s)
	if !strings.Contains(imageName, ":") {
		imageName += ":latest"
	}
	return imageName
}

// Initialize docker client and context
// The close() method has to be triggered with a "defer bw.Close()" just after the Init()
func (bw *BaseWatch) Init() error {
	bw.ctx = context.Background()
	cli, err := client.New(client.WithHostFromEnv(), client.WithAPIVersionFromEnv())
	if err != nil {
		return fmt.Errorf("failed to create Docker client: %v", err)
	}
	bw.client = cli

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load docker config: %v", err)
	}
	bw.dockerConfiguration = &cfg

	return nil
}

// To be used in a "defer" call
func (bw *BaseWatch) Close() error {
	err := bw.client.Close()
	if err != nil {
		return err
	}
	return nil
}

func (bw *BaseWatch) LoadAuthConfig(imageNames []string) error {
	authConfigs, err := config.AuthConfigs(imageNames...)
	if err != nil {
		return fmt.Errorf("failed to get registry credentials: %v", err)
	}
	bw.authConfigs = authConfigs

	// fmt.Println("!!!!!!!!!! registry credentials: %s ==> %+v", imageName, authConfigs)

	// fmt.Printf("docker config: %+v", cfg)
	// return authConfigs, nil
	return nil
}

func (bw *BaseWatch) RetrieveAuthConfig(imageName string) *registry.AuthConfig {
	for key, value := range bw.authConfigs {
		if strings.HasPrefix(imageName, key) {
			return &value
		}
	}
	return nil
}

// Check one image
func (bw *BaseWatch) CheckImage(imageName string) (*AnalyzeResult, error) {
	imageNameCleaned := strings.TrimSpace(imageName)
	imageNameToBeAnalyzed := addImageTagIfNeeded(imageNameCleaned)

	result := &AnalyzeResult{ImageNameSource: imageNameCleaned, ImageNameAnalyzed: imageNameToBeAnalyzed}

	// Get local image digest
	localInspect, err := bw.client.ImageInspect(bw.ctx, result.ImageNameAnalyzed)
	if err != nil {
		result.ErrorDuringAnalysis = true
		if strings.Contains(err.Error(), "No such image") {
			result.ErrorImageNotFoundInLocal = true
		}
		return result, fmt.Errorf("failed to inspect local image [%s] : %v", imageName, err)
	}

	var localDigest string
	if len(localInspect.RepoDigests) > 0 {
		for _, digest := range localInspect.RepoDigests {
			if strings.HasPrefix(digest, result.ImageNameAnalyzed[:strings.LastIndex(result.ImageNameAnalyzed, ":")]) {
				localDigest = strings.Split(digest, "@")[1]
				break
			}
		}
	} else {
		localDigest = localInspect.ID
	}

	authConfig := bw.RetrieveAuthConfig(imageName)
	opts := client.DistributionInspectOptions{}
	if authConfig != nil {
		authStr, err := authconfig.Encode(*authConfig)
		if err != nil {
			result.ErrorDuringAnalysis = true
			return result, fmt.Errorf("failed to prepare auth for image [%s] : %v", imageName, err)

		}
		if authStr != "" {
			opts.EncodedRegistryAuth = authStr
		}
	}

	// Get remote digest
	dist, err := bw.client.DistributionInspect(bw.ctx, result.ImageNameAnalyzed, opts)
	if err != nil {
		result.ErrorDuringAnalysis = true
		return result, fmt.Errorf("failed to inspect remote image [%s] : %v", imageName, err)
	}
	remoteDigest := dist.Descriptor.Digest.String()

	result.DigestLocal = localDigest
	result.DigestRemote = remoteDigest
	result.Identical = (localDigest == remoteDigest)

	if bw.Progress != nil {
		bw.Progress(*result)
	}

	return result, nil
}

func startThreads(nbThreads int, channelJobs *chan ExecutionWorker, wg *sync.WaitGroup) {
	for i := 0; i < nbThreads; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ch := range *channelJobs {
				ch.Execute()
			}
		}()
	}

}

// Check multiple images at one
// Errors have to be managed externlly check the Error status of AnalyzeResults)
func (bw *BaseWatch) CheckImages(imageNames []string) AnalyzeResults {

	// Start pool thread and associated channels
	channelJobs := make(chan ExecutionWorker, len(imageNames))
	channelResults := make(chan *AnalyzeResult, len(imageNames))
	wg := &sync.WaitGroup{}

	results := AnalyzeResults{}
	sortedImageNames := slices.Clone(lo.Uniq(imageNames))
	slices.Sort(sortedImageNames)

	bw.LoadAuthConfig(imageNames)

	startThreads(bw.NbThreads, &channelJobs, wg)

	for _, imageName := range sortedImageNames {

		// fmt.Println("Queing job :" + imageName)
		executionWorker := ExecutionWorker{
			Execute: func() {
				// fmt.Println("Executing job :" + imageName)
				result, err := bw.CheckImage(imageName)
				if err != nil {
					result.ImageNameSource = imageName
					result.Error = err
				}
				// fmt.Println("Sending result for : " + imageName)
				channelResults <- result

			},
		}
		channelJobs <- executionWorker
	}

	for i := 0; i < len(sortedImageNames); i++ {
		result := <-channelResults
		results = append(results, result)
	}

	// fmt.Println("Closing channel results  ...")
	close(channelResults)
	// fmt.Println("Closing channel jobs ...")
	close(channelJobs)
	// fmt.Println("Awaiting end of execution")
	wg.Wait()

	sort.Slice(results, func(i, j int) bool {
		return results[i].ImageNameSource < results[j].ImageNameSource
	})

	return results
}

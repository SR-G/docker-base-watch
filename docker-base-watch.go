package main

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/SR-G/argweave"
	"github.com/SR-G/sul"
	sulstrings "github.com/SR-G/sul/strings"
	"github.com/rs/zerolog"
	"github.com/samber/lo"
)

var logger zerolog.Logger

// These variables are injected through Makefile at compile time
var BuildVersion = "1.1.1"
var BuildVersionLabel = "DEV"
var BuildCommit = ""
var BuildCompilationTimestamp = ""

// Version string
var Version = sul.Version{ApplicationName: "docker-base-watch", Version: BuildVersion, VersionLabel: BuildVersionLabel, Commit: BuildCommit, CompilationTimestamp: BuildCompilationTimestamp}

const (
	DEFAULT_NUMBER_OF_THREADS = 20

	EXIT_VALUE_ALL_SAME_VERSION        = 0
	EXIT_VALUE_DIFFERENT_VERSION_FOUND = 1
	EXIT_VALUE_INVALID_ARGUMENTS       = 2
	EXIT_VALUE_NOTHING_DONE            = 3
	EXIT_VALUE_INTERNAL_ERROR          = 4

	EXECUTION_MODE_ANALYZE         = "ANALYZE"
	EXECUTION_MODE_DISPLAY_VERSION = "VERSION"

	RESULT_STATUS_ERROR_NOT_FOUND_IN_LOCAL = "NOT_FOUND_IN_LOCAL"
	RESULT_STATUS_UPDATE_AVAILABLE         = "UPDATE_AVAILABLE"
	RESULT_STATUS_NO_UPDATE_AVAILABLE      = "UP_TO_DATE"
	RESULT_STATUS_ERROR                    = "ERROR"
)

//go:generate go run github.com/SR-G/argweave/cmd/weavedoc -type=Options -file=docker-base-watch.go -marker=weavedoc -edit=README.md
//go:generate go run github.com/SR-G/argweave/cmd/weavehelp -type=Options -file=docker-base-watch.go -name=docker-base-watch -marker=weavehelp -disable-version -no-config-flag -edit=README.md
type Options struct {
	Image          string   `arg:"short=i,long=image,help=Docker image name to check (in this mode\\, only one information is displayed at the output\\, about the status of that image)"`
	ImageNames     []string `arg:"long=images,help=Docker image names to check (in this mode\\, all images are checked\\, and the output is displaying the list of all images having some updates available)"`
	ExcludedImages []string `arg:"long=excluded-images,help=Docker image names to exclude (regexp\\, allowing to exclude some of the queried images)"`
	Quiet          bool     `arg:"short=q,long=quiet,help=Enable quiet output (final result to be retrieved only through exit value\\, i.e.\\, echo $?)"`
	LogDebug       bool     `arg:"short=v,long=verbose,help=Enable verbose output"`
	DisplayVersion bool     `arg:"long=version,help=Display program version"`
	JSONLogs       bool     `arg:"long=jon-logs,help=logs printed as JSON"`
	NbThreads      int      `arg:"short=t,long=threads,env=DOCKER_BASE_WATCH_NB_THREADS,default=20,help=Number of concurrent threads running in parallel (warning : too many threads may lead to errors like 429\\, etc.)"`

	ExecutionMode string
}

// Validate is called by argweave once all values are resolved.
func (o *Options) Validate() []error {
	var errs []error
	if o.NbThreads < 1 {
		errs = append(errs, fmt.Errorf("--threads must be >= 1, got %d", o.NbThreads))
	}
	for _, pattern := range splitImages(o.ExcludedImages) {
		if _, err := regexp.Compile(pattern); err != nil {
			errs = append(errs, fmt.Errorf("--excluded-images: can't compile [%s] as a regexp: %w", pattern, err))
		}
	}
	if !o.DisplayVersion && len(splitImages(append([]string{o.Image}, o.ImageNames...))) == 0 {
		errs = append(errs, errors.New("image name to analyse is mandatory (either through '--image' or '--images')"))
	}
	return errs
}

type ExecutionWorker struct {
	Execute func()
}

func newLogger(jsonLogs, debug, silent bool) zerolog.Logger {
	if silent {
		return zerolog.Nop()
	}

	var l zerolog.Logger
	if jsonLogs {
		l = zerolog.New(os.Stderr).With().Timestamp().Logger()
	} else {
		writer := zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339, NoColor: true}
		writer.FormatLevel = func(i interface{}) string {
			return strings.ToUpper(fmt.Sprintf("%-6s", i))
		}
		// writer.FormatMessage = func(i interface{}) string {
		//			return fmt.Sprintf("***%s****", i)
		// }
		writer.FormatFieldName = func(i interface{}) string {
			return fmt.Sprintf("%s ", i)
		}
		writer.FormatFieldValue = func(i interface{}) string {
			return fmt.Sprintf("[%s]", i)
		}

		writer.PartsOrder = []string{
			zerolog.LevelFieldName,
			zerolog.TimestampFieldName,
			zerolog.CallerFieldName,
			zerolog.MessageFieldName,
		}

		l = zerolog.New(writer).With().Timestamp().Logger()
	}
	if debug {
		l = l.Level(zerolog.DebugLevel)
	} else {
		l = l.Level(zerolog.InfoLevel)
	}

	return l
}

func initBaseWatch(options *Options) *BaseWatch {
	result := NewBaseWatch()
	err := result.Init()
	if err != nil {
		logger.Error().Err(err).Msg("Can't initialize docker client")
		os.Exit(EXIT_VALUE_INTERNAL_ERROR)
	}
	result.Progress = func(result AnalyzeResult) {
		s := "[DIFFERENT]"
		if result.Identical {
			s = "[IDENTICAL]"
		}
		logger.Debug().Str("analyzed", result.ImageNameAnalyzed).Str("local", result.DigestLocal).Str("remote", result.DigestRemote).Msg(s + " source image name [" + result.ImageNameSource + "]")

	}
	result.NbThreads = options.NbThreads
	return result
}

func isExcluded(regexps []*regexp.Regexp, s string) bool {
	for _, r := range regexps {
		if r.MatchString(s) {
			return true
		}
	}
	return false
}

// splitImages splits comma/space-separated values and drops empty entries.
func splitImages(values []string) []string {
	result := make([]string, 0)
	for _, value := range values {
		for _, part := range sulstrings.SplitAny(value, ", ") {
			if cleaned := strings.TrimSpace(part); cleaned != "" {
				result = append(result, cleaned)
			}
		}
	}
	return result
}

func initOptions() *Options {
	options := &Options{}

	parser, err := argweave.New(options, argweave.AppConfig{
		Name:        Version.ApplicationName,
		Version:     Version.String(),
		Description: "Checks if docker images have newer versions available in upstream registries.",
		// Flags override ENV; no config file
		Providers:      []argweave.ProviderKind{argweave.ProviderFlags, argweave.ProviderEnv, argweave.ProviderDefault},
		DisableVersion: true,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(EXIT_VALUE_INTERNAL_ERROR)
	}
	parser.ParseAndHandleExitIfNeeded(os.Args[1:])

	if options.DisplayVersion {
		options.ExecutionMode = EXECUTION_MODE_DISPLAY_VERSION
		return options
	}
	options.ExecutionMode = EXECUTION_MODE_ANALYZE

	logger = newLogger(options.JSONLogs, options.LogDebug, options.Quiet)
	logger.Debug().Msg("Verbose mode enabled")

	// Exclusions were already validated as regexps by Validate()
	exclusions := lo.Map(splitImages(options.ExcludedImages), func(pattern string, _ int) *regexp.Regexp {
		return regexp.MustCompile(pattern)
	})

	imageNames := splitImages(options.ImageNames)
	imageNames = lo.Filter(imageNames, func(name string, _ int) bool { return !isExcluded(exclusions, name) })
	// --image is never subject to exclusions
	options.ImageNames = append(splitImages([]string{options.Image}), imageNames...)
	options.ImageNames = lo.Uniq(options.ImageNames)
	slices.Sort(options.ImageNames)

	if len(options.ImageNames) == 0 {
		logger.Error().Msg("All provided images are excluded, nothing to analyse")
		os.Exit(EXIT_VALUE_INVALID_ARGUMENTS)
	}
	logger.Debug().Int("nb", options.NbThreads).Msg("Starting program with multiple threads,")

	return options
}

func main() {
	startTime := time.Now()
	options := initOptions()

	switch options.ExecutionMode {
	case EXECUTION_MODE_DISPLAY_VERSION:
		fmt.Println(Version.String())
		os.Exit(EXIT_VALUE_NOTHING_DONE)
	case EXECUTION_MODE_ANALYZE:
		baseWatch := initBaseWatch(options)
		defer baseWatch.Close()

		results := baseWatch.CheckImages(options.ImageNames)
		imagesFoundBeingDifferent := results.NumberOfDifferentImages()
		errors := results.NumberOfAnalysisInError()
		// In case of debug, display all details about found errors (Stderr)
		if options.LogDebug {
			for _, result := range results {
				if result.Error != nil {
					logger.Debug().Str("image", result.ImageNameSource).Err(result.Error).Msg("Error checking image version")
				}
			}
		}
		// If we are not quiet, display the summary (Stdout)
		if !options.Quiet {
			for _, result := range results {
				fmt.Println(fmt.Sprintf("%-19s", result.Status()) + result.ImageNameSource)
			}
		}
		elapsed := time.Since(startTime)
		logger.Debug().Int("analyzed images", len(options.ImageNames)).Int("threads", options.NbThreads).Str("elapsed", sulstrings.HumanizeDuration(elapsed)).Msg("End of execution :")
		if errors > 0 {
			os.Exit(EXIT_VALUE_INTERNAL_ERROR)
		} else {
			if imagesFoundBeingDifferent > 0 {
				os.Exit(EXIT_VALUE_DIFFERENT_VERSION_FOUND)
			} else {
				os.Exit(EXIT_VALUE_ALL_SAME_VERSION)
			}
		}
	}
}

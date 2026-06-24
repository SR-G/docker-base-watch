package main

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/SR-G/sul"
	"github.com/integrii/flaggy"
	"github.com/rs/zerolog"
	"github.com/samber/lo"
)

var logger zerolog.Logger

const (
	PROGRAM_NAME          = "docker-base-watch"
	PROGRAM_VERSION       = "1.1.1"    // overwritten at release time from makefile
	PROGRAM_VERSION_LABEL = "SNAPSHOT" // overwritten at release time from makefile

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

type Options struct {
	ImageNames    []string
	ExecutionMode string
	NbThreads     int
	Quiet         bool
	LogDebug      bool
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

func initOptions() *Options {
	options := &Options{}

	var parameterVersion bool

	var parameterVerbose bool
	var parameterQuiet bool
	var parameterJSONLogs bool

	var parameterImageName string
	var parameterImageNames string

	var parameterNbConcurrentThreads = sul.GetEnvWithDefaultInt("DOCKER_BASE_WATCH_NB_THREADS", 1)

	flaggy.String(&parameterImageName, "i", "image", "Docker image name to check (in this mode, only one information is displayed at the output, about the status of that image)")
	flaggy.String(&parameterImageNames, "", "images", "Docker image names to check (in this mode, all images are checked, and the output is displaying the list of all images having some updates available)")

	flaggy.Bool(&parameterQuiet, "q", "quiet", "Enable quiet output (final result to be retrieved only through exit value, i.e., echo $?)")
	flaggy.Bool(&parameterVerbose, "v", "verbose", "Enable verbose output")
	flaggy.Bool(&parameterVersion, "", "version", "Display program version")
	flaggy.Bool(&parameterJSONLogs, "", "jon-logs", "logs printed as JSON")

	flaggy.Int(&parameterNbConcurrentThreads, "t", "threads", "Number of concurrent threads running in parallel (warning : too many threads may lead to errors like 429, etc.)")

	flaggy.DefaultParser.ShowVersionWithVersionFlag = false
	flaggy.SetName(Version.ApplicationName)
	flaggy.SetVersion(Version.String())
	flaggy.Parse()

	if parameterVersion {
		options.ExecutionMode = EXECUTION_MODE_DISPLAY_VERSION
	} else {
		options.ExecutionMode = EXECUTION_MODE_ANALYZE
	}

	logger = newLogger(parameterJSONLogs, parameterVerbose, parameterQuiet)
	if parameterVerbose {
		logger.Debug().Msg("Verbose mode enabled")
	}
	options.LogDebug = parameterVerbose

	// Add options
	options.ImageNames = make([]string, 0)
	imageNameCleaned := strings.TrimSpace(parameterImageName)
	if imageNameCleaned != "" {
		options.ImageNames = append(options.ImageNames, strings.TrimSpace(imageNameCleaned))
	}

	for _, imageName := range sul.SplitAny(parameterImageNames, ", ") {
		imageNameCleaned := strings.TrimSpace(imageName)
		if imageNameCleaned != "" {
			options.ImageNames = append(options.ImageNames, strings.TrimSpace(imageNameCleaned))
		}
	}

	options.Quiet = parameterQuiet
	options.NbThreads = 1
	if parameterNbConcurrentThreads > 1 {
		options.NbThreads = parameterNbConcurrentThreads
	}
	logger.Debug().Int("nb", options.NbThreads).Msg("Starting program with multiple threads,")
	options.ImageNames = lo.Uniq(options.ImageNames)
	slices.Sort(options.ImageNames)

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
		if len(options.ImageNames) == 0 {
			logger.Error().Msg("Image name to analyse is mandatory (either through '--image' or '--images')")
			flaggy.DefaultParser.ShowHelp()
			os.Exit(EXIT_VALUE_INVALID_ARGUMENTS)
		}

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
		logger.Debug().Int("analyzed images", len(options.ImageNames)).Int("threads", options.NbThreads).Str("elapsed", sul.HumanizeDuration(elapsed)).Msg("End of execution :")
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

package main

import (
	"DataArk/articlevalue"
	"DataArk/assessmenteval"
	"DataArk/recommendation"
	"context"
	"errors"
	stdflag "flag"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "article-assessment-eval:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printUsage(stderr)
		return errors.New("a command is required")
	}
	switch args[0] {
	case "sample":
		return runSample(args[1:], stdout, stderr)
	case "pass2":
		return runPassTwo(args[1:], stdout, stderr)
	case "adjudicate":
		return runAdjudication(args[1:], stdout, stderr)
	case "score":
		return runScore(args[1:], stdout, stderr)
	case "report":
		return runReport(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printUsage(stdout)
		return nil
	default:
		printUsage(stderr)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runSample(args []string, stdout, stderr io.Writer) error {
	flags := stdflag.NewFlagSet("sample", stdflag.ContinueOnError)
	flags.SetOutput(stderr)
	outDirectory := flags.String("out-dir", "", "private output directory (default: a new directory under /tmp)")
	seed := flags.String("seed", "article-value-v3-gold-v1", "stable deterministic sampling seed")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *outDirectory == "" {
		created, err := os.MkdirTemp("", "dataark-article-assessment-")
		if err != nil {
			return err
		}
		*outDirectory = created
	}
	database, err := openDatabaseFromEnvironment()
	if err != nil {
		return err
	}
	records, err := assessmenteval.LoadCandidateRecords(database)
	if err != nil {
		return err
	}
	manifest, err := assessmenteval.BuildManifest(records, *seed, time.Now())
	if err != nil {
		return err
	}
	page, err := assessmenteval.BuildPassOneHTML(manifest)
	if err != nil {
		return err
	}
	manifestPath := filepath.Join(*outDirectory, "manifest.json")
	pagePath := filepath.Join(*outDirectory, "pass1.html")
	if err := assessmenteval.WritePrivateJSON(manifestPath, manifest); err != nil {
		return err
	}
	if err := assessmenteval.WritePrivateFile(pagePath, page); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "sampled %d immutable article versions\nmanifest: %s\npass one: %s\n", len(manifest.Items), manifestPath, pagePath)
	return nil
}

func runPassTwo(args []string, stdout, stderr io.Writer) error {
	flags := stdflag.NewFlagSet("pass2", stdflag.ContinueOnError)
	flags.SetOutput(stderr)
	manifestPath := flags.String("manifest", "", "path to the private manifest.json")
	passOnePath := flags.String("pass1-labels", "", "exported pass-one label JSON")
	outputPath := flags.String("out", "", "pass-two HTML path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	manifest, passOne, err := readManifestAndLabels(*manifestPath, *passOnePath)
	if err != nil {
		return err
	}
	payload, ids, err := assessmenteval.BuildPassTwoHTML(manifest, passOne, time.Now())
	if err != nil {
		return err
	}
	if *outputPath == "" {
		*outputPath = filepath.Join(filepath.Dir(*manifestPath), "pass2.html")
	}
	if err := assessmenteval.WritePrivateFile(*outputPath, payload); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "created blind pass two with %d articles: %s\n", len(ids), *outputPath)
	return nil
}

func runAdjudication(args []string, stdout, stderr io.Writer) error {
	flags := stdflag.NewFlagSet("adjudicate", stdflag.ContinueOnError)
	flags.SetOutput(stderr)
	manifestPath := flags.String("manifest", "", "path to the private manifest.json")
	passOnePath := flags.String("pass1-labels", "", "exported pass-one label JSON")
	passTwoPath := flags.String("pass2-labels", "", "exported pass-two label JSON")
	outputPath := flags.String("out", "", "adjudication HTML path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	manifest, passOne, err := readManifestAndLabels(*manifestPath, *passOnePath)
	if err != nil {
		return err
	}
	passTwo, err := assessmenteval.ReadLabels(requiredPath("pass2-labels", *passTwoPath))
	if err != nil {
		return err
	}
	payload, ids, err := assessmenteval.BuildAdjudicationHTML(manifest, passOne, passTwo)
	if err != nil {
		return err
	}
	if *outputPath == "" {
		*outputPath = filepath.Join(filepath.Dir(*manifestPath), "adjudication.html")
	}
	if err := assessmenteval.WritePrivateFile(*outputPath, payload); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "created adjudication page with %d conflicts: %s\n", len(ids), *outputPath)
	return nil
}

func runScore(args []string, stdout, stderr io.Writer) error {
	flags := stdflag.NewFlagSet("score", stdflag.ContinueOnError)
	flags.SetOutput(stderr)
	manifestPath := flags.String("manifest", "", "path to the private manifest.json")
	outputPath := flags.String("out", "", "score JSON path")
	logPath := flags.String("llm-log", "", "structured per-call log path")
	concurrency := flags.Int("concurrency", 2, "maximum concurrent assessment calls")
	repeat := flags.Int("repeat", 2, "number of model runs per article (1 or 2)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	manifest, err := assessmenteval.ReadManifest(requiredPath("manifest", *manifestPath))
	if err != nil {
		return err
	}
	if *concurrency < 1 || *concurrency > 16 {
		return errors.New("concurrency must be between 1 and 16")
	}
	if *repeat < 1 || *repeat > 2 {
		return errors.New("repeat must be 1 or 2")
	}
	baseURL, model := strings.TrimSpace(os.Getenv("LLM_BASE_URL")), strings.TrimSpace(os.Getenv("LLM_CHAT_MODEL"))
	if model == "" {
		return errors.New("LLM_CHAT_MODEL is required")
	}
	timeout, err := time.ParseDuration(environmentDefault("LLM_TIMEOUT", "30s"))
	if err != nil {
		return fmt.Errorf("parse LLM_TIMEOUT: %w", err)
	}
	provider := recommendation.OpenAICompatibleProvider{BaseURL: baseURL, APIKey: os.Getenv("LLM_API_KEY"), ChatModel: model, Timeout: timeout}
	if *outputPath == "" {
		*outputPath = filepath.Join(filepath.Dir(*manifestPath), "scores.json")
	}
	if *logPath == "" {
		*logPath = filepath.Join(filepath.Dir(*manifestPath), "llm-calls.log")
	}
	if err := os.MkdirAll(filepath.Dir(*logPath), 0o700); err != nil {
		return err
	}
	if err := os.Chmod(filepath.Dir(*logPath), 0o700); err != nil {
		return err
	}
	logFile, err := os.OpenFile(*logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := logFile.Chmod(0o600); err != nil {
		logFile.Close()
		return err
	}
	defer logFile.Close()
	previousLogWriter := log.Writer()
	log.SetOutput(io.MultiWriter(stderr, logFile))
	defer log.SetOutput(previousLogWriter)

	type workItem struct {
		index, run int
		item       assessmenteval.ManifestItem
	}
	jobs := make(chan workItem)
	records := make([]assessmenteval.ScoreRecord, len(manifest.Items)**repeat)
	var wait sync.WaitGroup
	for worker := 0; worker < *concurrency; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for job := range jobs {
				started := time.Now()
				result, callErr := provider.AssessArticle(context.Background(), recommendation.ArticleAssessmentInput{CandidateID: job.item.CandidateID, Title: job.item.Title, BodyText: job.item.BodyText})
				record := assessmenteval.ScoreRecord{SampleID: job.item.SampleID, CandidateID: job.item.CandidateID, Run: job.run, DurationMilliseconds: time.Since(started).Milliseconds()}
				if callErr != nil {
					record.Error = compactError(callErr)
				} else {
					capped := articlevalue.ApplyEvidenceCaps(articlevalue.Scores{Quality: float64(result.QualityScore) / 100, Depth: float64(result.DepthScore) / 100, Evergreen: float64(result.EvergreenScore) / 100}, result.OriginalEvidenceTokens)
					record.Scores = assessmenteval.AxisScores{Quality: int(math.Round(capped.Quality * 100)), Depth: int(math.Round(capped.Depth * 100)), Evergreen: int(math.Round(capped.Evergreen * 100))}
					record.Reasons = result.Reasons
					record.EvidenceTokens, record.OriginalEvidenceTokens, record.EvidenceTruncated = result.EvidenceTokens, result.OriginalEvidenceTokens, result.EvidenceTruncated
				}
				records[job.index] = record
			}
		}()
	}
	index := 0
	for runNumber := 1; runNumber <= *repeat; runNumber++ {
		for _, item := range manifest.Items {
			jobs <- workItem{index: index, run: runNumber, item: item}
			index++
		}
	}
	close(jobs)
	wait.Wait()
	scores := assessmenteval.ScoreSet{Version: assessmenteval.ScoreVersion, ManifestDigest: manifest.Digest, Model: model, PromptVersion: articlevalue.PromptVersion, CreatedAt: time.Now().UTC(), Records: records}
	if err := assessmenteval.WritePrivateJSON(*outputPath, scores); err != nil {
		return err
	}
	failed := 0
	for _, record := range records {
		if record.Error != "" {
			failed++
		}
	}
	fmt.Fprintf(stdout, "scored %d calls with %d failures\nscores: %s\ncall log: %s\n", len(records), failed, *outputPath, *logPath)
	if failed > 0 {
		return fmt.Errorf("%d assessment calls failed; partial private artifacts were retained", failed)
	}
	return nil
}

func runReport(args []string, stdout, stderr io.Writer) error {
	flags := stdflag.NewFlagSet("report", stdflag.ContinueOnError)
	flags.SetOutput(stderr)
	manifestPath := flags.String("manifest", "", "path to the private manifest.json")
	passOnePath := flags.String("pass1-labels", "", "exported pass-one label JSON")
	passTwoPath := flags.String("pass2-labels", "", "exported pass-two label JSON")
	scorePath := flags.String("scores", "", "model score JSON")
	logPath := flags.String("llm-log", "", "structured model call log")
	adjudicationPath := flags.String("adjudication", "", "optional exported adjudication JSON")
	outputPath := flags.String("out", "", "report JSON path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	manifest, passOne, err := readManifestAndLabels(*manifestPath, *passOnePath)
	if err != nil {
		return err
	}
	passTwo, err := assessmenteval.ReadLabels(requiredPath("pass2-labels", *passTwoPath))
	if err != nil {
		return err
	}
	scores, err := assessmenteval.ReadScores(requiredPath("scores", *scorePath))
	if err != nil {
		return err
	}
	var adjudication *assessmenteval.LabelSet
	if strings.TrimSpace(*adjudicationPath) != "" {
		labels, readErr := assessmenteval.ReadLabels(*adjudicationPath)
		if readErr != nil {
			return readErr
		}
		adjudication = &labels
	}
	var callLog io.Reader
	var callLogFile *os.File
	if strings.TrimSpace(*logPath) != "" {
		callLogFile, err = os.Open(*logPath)
		if err != nil {
			return err
		}
		defer callLogFile.Close()
		callLog = callLogFile
	}
	report, err := assessmenteval.BuildReport(manifest, passOne, passTwo, adjudication, scores, callLog, time.Now())
	if err != nil {
		return err
	}
	if *outputPath == "" {
		*outputPath = filepath.Join(filepath.Dir(*manifestPath), "report.json")
	}
	if err := assessmenteval.WritePrivateJSON(*outputPath, report); err != nil {
		return err
	}
	failedChecks := make([]string, 0)
	for name, passed := range report.ActivationChecks {
		if !passed {
			failedChecks = append(failedChecks, name)
		}
	}
	sort.Strings(failedChecks)
	fmt.Fprintf(stdout, "activationReady=%t unresolvedConflicts=%d failedChecks=%s\nreport: %s\n", report.ActivationReady, report.UnresolvedConflicts, strings.Join(failedChecks, ","), *outputPath)
	return nil
}

func openDatabaseFromEnvironment() (*gorm.DB, error) {
	if dsn := strings.TrimSpace(os.Getenv("DATAARK_ASSESSMENT_DSN")); dsn != "" {
		return gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	}
	databaseName, user := strings.TrimSpace(os.Getenv("DB_NAME")), strings.TrimSpace(os.Getenv("DB_USER"))
	if databaseName == "" || user == "" {
		return nil, errors.New("set DATAARK_ASSESSMENT_DSN or DB_NAME and DB_USER; DB_HOST, DB_PORT, DB_PASSWORD and DB_SSLMODE are optional")
	}
	dsn := fmt.Sprintf("host=%s port=%s dbname=%s user=%s password=%s sslmode=%s", environmentDefault("DB_HOST", "localhost"), environmentDefault("DB_PORT", "5432"), databaseName, user, os.Getenv("DB_PASSWORD"), environmentDefault("DB_SSLMODE", "disable"))
	return gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
}

func readManifestAndLabels(manifestPath, labelPath string) (assessmenteval.Manifest, assessmenteval.LabelSet, error) {
	manifest, err := assessmenteval.ReadManifest(requiredPath("manifest", manifestPath))
	if err != nil {
		return manifest, assessmenteval.LabelSet{}, err
	}
	labels, err := assessmenteval.ReadLabels(requiredPath("labels", labelPath))
	return manifest, labels, err
}

func requiredPath(name, path string) string {
	if strings.TrimSpace(path) == "" {
		return filepath.Join("missing-required-argument", name)
	}
	return path
}

func environmentDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func compactError(err error) string {
	message := strings.Join(strings.Fields(err.Error()), " ")
	runes := []rune(message)
	if len(runes) > 300 {
		message = string(runes[:300])
	}
	return message
}

func printUsage(writer io.Writer) {
	fmt.Fprintln(writer, `Usage: article-assessment-eval <command> [options]

Commands:
  sample       Select the deterministic 120-article gold set from current immutable DB versions.
  pass2        Create the blind 30-article second pass after the enforced 72-hour interval.
  adjudicate   Create a blind page containing only cross-band or >15-point repeat conflicts.
  score        Score the private manifest with the configured OpenAI-compatible model.
  report       Compare human, model, stored baseline, repeat stability, tokens, and activation gates.

Credentials are read only from environment variables. sample accepts DATAARK_ASSESSMENT_DSN or
DB_HOST/DB_PORT/DB_NAME/DB_USER/DB_PASSWORD/DB_SSLMODE. score accepts LLM_BASE_URL,
LLM_API_KEY, LLM_CHAT_MODEL, and LLM_TIMEOUT. Generated artifacts contain article text or labels,
are written with private permissions, and must remain outside Git.`)
}

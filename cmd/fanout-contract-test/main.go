package main

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const reportMarker = "<!-- distributed-infra-app-contract-report -->"

type serviceRoute struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Owner        string `json:"owner"`
	Repository   string `json:"repository"`
	ContractPath string `json:"contract_path"`
	ContractTest struct {
		Enabled   bool   `json:"enabled"`
		EventType string `json:"event_type"`
	} `json:"contract_test"`
}

type registryIndex struct {
	Services []serviceRoute `json:"services"`
}

type violation struct {
	RuleID      string `json:"rule_id"`
	Severity    string `json:"severity"`
	Evidence    string `json:"evidence"`
	Reason      string `json:"reason"`
	Remediation string `json:"remediation"`
}

type serviceResult struct {
	SchemaVersion string      `json:"schema_version"`
	CorrelationID string      `json:"correlation_id"`
	ServiceID     string      `json:"service_id"`
	ServiceName   string      `json:"service_name"`
	Owner         string      `json:"owner"`
	Repository    string      `json:"repository"`
	Status        string      `json:"status"`
	Risk          string      `json:"risk"`
	Summary       string      `json:"summary"`
	Engine        string      `json:"engine"`
	Violations    []violation `json:"violations"`
	WorkflowURL   string      `json:"workflow_url,omitempty"`
}

type aggregateReport struct {
	SchemaVersion string          `json:"schema_version"`
	CorrelationID string          `json:"correlation_id"`
	InfraRepo     string          `json:"infra_repository"`
	PRNumber      int             `json:"infra_pr_number"`
	HeadSHA       string          `json:"infra_head_sha"`
	Status        string          `json:"status"`
	Summary       string          `json:"summary"`
	Services      []serviceResult `json:"services"`
}

type config struct {
	RegistryRepo string
	RegistryRef  string
	RegistryDir  string
	ResultsDir   string
	Output       string
	Timeout      time.Duration
	PollInterval time.Duration
	DryRun       bool
}

type pullRequestEvent struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	HTMLURL     string `json:"html_url"`
	PullRequest struct {
		Number  int    `json:"number"`
		Title   string `json:"title"`
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
		Head    struct {
			SHA string `json:"sha"`
		} `json:"head"`
	} `json:"pull_request"`
}

type githubArtifact struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Expired     bool      `json:"expired"`
	CreatedAt   time.Time `json:"created_at"`
	WorkflowRun struct {
		ID int64 `json:"id"`
	} `json:"workflow_run"`
}

var httpClient = &http.Client{Timeout: 60 * time.Second}

func main() {
	code, err := run(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Distributed contract test failed: %v\n", err)
		if code == 0 {
			code = 2
		}
	}
	os.Exit(code)
}

func run(args []string) (int, error) {
	cfg, err := parseConfig(args)
	if err != nil {
		return 2, err
	}
	index, err := loadRegistry(cfg)
	if err != nil {
		return 2, err
	}
	routes, err := enabledRoutes(index.Services)
	if err != nil {
		return 2, err
	}

	event, err := loadEvent(cfg)
	if err != nil {
		return 2, err
	}
	infraRepo := env("GITHUB_REPOSITORY", "local/demo-infra-platform")
	correlation := buildCorrelation(infraRepo, event.PullRequest.Number, event.PullRequest.Head.SHA, env("GITHUB_RUN_ID", "local"), env("GITHUB_RUN_ATTEMPT", "1"))
	changeDescription := strings.TrimSpace(event.PullRequest.Title + "\n\n" + event.PullRequest.Body)
	started := time.Now().UTC()

	var results []serviceResult
	if cfg.ResultsDir != "" {
		results, err = loadLocalResults(routes, cfg.ResultsDir, correlation)
	} else {
		token := env("FANOUT_TOKEN", "")
		if token == "" {
			return 2, errors.New("FANOUT_TOKEN is required for cross-repository dispatch")
		}
		dispatched := make([]serviceRoute, 0, len(routes))
		for _, route := range routes {
			if err := dispatch(route, token, correlation, infraRepo, event, changeDescription, cfg); err != nil {
				results = append(results, systemFailure(route, correlation, "CONTRACT-DISPATCH-ERROR", "The infra workflow could not start this service's contract test.", err.Error()))
				continue
			}
			dispatched = append(dispatched, route)
		}
		collected, collectErr := collectResults(dispatched, token, correlation, started, cfg)
		results = append(results, collected...)
		err = collectErr
	}
	if err != nil {
		return 2, err
	}
	sort.Slice(results, func(i, j int) bool { return results[i].ServiceID < results[j].ServiceID })

	report := aggregate(results, correlation, infraRepo, event.PullRequest.Number, event.PullRequest.Head.SHA)
	encoded, _ := json.MarshalIndent(report, "", "  ")
	if err := os.WriteFile(cfg.Output, append(encoded, '\n'), 0o644); err != nil {
		return 2, fmt.Errorf("write aggregate report: %w", err)
	}
	markdown := renderMarkdown(report)
	fmt.Println(markdown)
	if !cfg.DryRun && os.Getenv("GITHUB_ACTIONS") == "true" {
		if err := publish(markdown, report, env("GITHUB_TOKEN", "")); err != nil {
			return 2, err
		}
	}
	if report.Status == "fail" {
		return 1, nil
	}
	return 0, nil
}

func parseConfig(args []string) (config, error) {
	fs := flag.NewFlagSet("fanout-contract-test", flag.ContinueOnError)
	var cfg config
	var timeoutSeconds, pollSeconds int
	fs.StringVar(&cfg.RegistryRepo, "registry-repo", env("REGISTRY_REPO", ""), "owner/repository containing registry.json")
	fs.StringVar(&cfg.RegistryRef, "registry-ref", env("REGISTRY_REF", "main"), "registry git ref")
	fs.StringVar(&cfg.RegistryDir, "registry-dir", "", "local registry directory")
	fs.StringVar(&cfg.ResultsDir, "results-dir", "", "local service result directory (simulation mode)")
	fs.StringVar(&cfg.Output, "output", env("OUTPUT_PATH", ".contract-report.json"), "aggregate JSON output")
	fs.IntVar(&timeoutSeconds, "timeout-seconds", envInt("FANOUT_TIMEOUT_SECONDS", 600), "maximum fan-in wait")
	fs.IntVar(&pollSeconds, "poll-seconds", envInt("FANOUT_POLL_SECONDS", 10), "artifact polling interval")
	fs.BoolVar(&cfg.DryRun, "dry-run", false, "do not publish the aggregate result")
	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	if cfg.RegistryDir == "" && cfg.RegistryRepo == "" {
		return cfg, errors.New("--registry-repo or --registry-dir is required")
	}
	if timeoutSeconds < 1 || pollSeconds < 1 {
		return cfg, errors.New("timeout and poll intervals must be positive")
	}
	cfg.Timeout = time.Duration(timeoutSeconds) * time.Second
	cfg.PollInterval = time.Duration(pollSeconds) * time.Second
	return cfg, nil
}

func loadRegistry(cfg config) (registryIndex, error) {
	var index registryIndex
	var data []byte
	var err error
	if cfg.RegistryDir != "" {
		data, err = os.ReadFile(filepath.Join(cfg.RegistryDir, "registry.json"))
	} else {
		route := fmt.Sprintf("/repos/%s/contents/registry.json?ref=%s", cfg.RegistryRepo, url.QueryEscape(cfg.RegistryRef))
		var response struct {
			Content string `json:"content"`
		}
		err = githubJSON(http.MethodGet, route, env("REGISTRY_TOKEN", env("FANOUT_TOKEN", "")), nil, &response)
		if err == nil {
			data, err = decodeBase64(response.Content)
		}
	}
	if err != nil {
		return index, fmt.Errorf("load registry: %w", err)
	}
	if err := json.Unmarshal(data, &index); err != nil {
		return index, fmt.Errorf("parse registry: %w", err)
	}
	return index, nil
}

func enabledRoutes(services []serviceRoute) ([]serviceRoute, error) {
	routes := make([]serviceRoute, 0, len(services))
	seen := map[string]bool{}
	for _, route := range services {
		if !route.ContractTest.Enabled {
			continue
		}
		if route.ID == "" || route.Repository == "" {
			return nil, errors.New("enabled service route is missing id or repository")
		}
		if seen[route.ID] {
			return nil, fmt.Errorf("duplicate enabled service id %q", route.ID)
		}
		seen[route.ID] = true
		if route.ContractTest.EventType == "" {
			route.ContractTest.EventType = "infra_contract_test"
		}
		routes = append(routes, route)
	}
	if len(routes) == 0 {
		return nil, errors.New("registry has no enabled service contract test routes")
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].ID < routes[j].ID })
	return routes, nil
}

func loadEvent(cfg config) (pullRequestEvent, error) {
	var event pullRequestEvent
	if path := os.Getenv("GITHUB_EVENT_PATH"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return event, err
		}
		if err := json.Unmarshal(data, &event); err != nil {
			return event, err
		}
	}
	if event.PullRequest.Number == 0 {
		event.PullRequest.Number = envInt("PR_NUMBER", 1)
		event.PullRequest.Title = env("PR_TITLE", "Local contract simulation")
		event.PullRequest.Body = env("PR_BODY", "Local fan-out/fan-in simulation")
		event.PullRequest.Head.SHA = env("HEAD_SHA", "local")
	}
	return event, nil
}

func dispatch(route serviceRoute, token, correlation, infraRepo string, event pullRequestEvent, description string, cfg config) error {
	repository := qualifyRepository(route.Repository, infraRepo)
	payload := map[string]any{
		"event_type": route.ContractTest.EventType,
		"client_payload": map[string]any{
			"correlation_id":      correlation,
			"infra_repository":    infraRepo,
			"infra_pr_number":     event.PullRequest.Number,
			"infra_head_sha":      event.PullRequest.Head.SHA,
			"change_description":  truncate(description, 12000),
			"service_id":          route.ID,
			"registry_repository": cfg.RegistryRepo,
			"registry_ref":        cfg.RegistryRef,
		},
	}
	return githubJSON(http.MethodPost, fmt.Sprintf("/repos/%s/dispatches", repository), token, payload, nil)
}

func collectResults(routes []serviceRoute, token, correlation string, started time.Time, cfg config) ([]serviceResult, error) {
	pending := map[string]serviceRoute{}
	for _, route := range routes {
		pending[route.ID] = route
	}
	results := make([]serviceResult, 0, len(routes))
	deadline := time.Now().Add(cfg.Timeout)
	for len(pending) > 0 && time.Now().Before(deadline) {
		for id, route := range pending {
			repository := qualifyRepository(route.Repository, env("GITHUB_REPOSITORY", ""))
			artifactName := artifactName(correlation, id)
			artifact, found, err := findArtifact(repository, artifactName, token, started)
			if err != nil {
				return nil, fmt.Errorf("poll %s: %w", id, err)
			}
			if !found {
				continue
			}
			result, err := downloadResult(repository, artifact, token)
			if err != nil {
				results = append(results, systemFailure(route, correlation, "CONTRACT-RESULT-ERROR", "The service workflow returned an unreadable contract result.", err.Error()))
				delete(pending, id)
				continue
			}
			if err := validateResult(result, route, correlation); err != nil {
				results = append(results, systemFailure(route, correlation, "CONTRACT-RESULT-ERROR", "The service workflow returned an invalid contract result.", err.Error()))
				delete(pending, id)
				continue
			}
			result.WorkflowURL = fmt.Sprintf("https://github.com/%s/actions/runs/%d", repository, artifact.WorkflowRun.ID)
			results = append(results, result)
			delete(pending, id)
		}
		if len(pending) > 0 {
			time.Sleep(cfg.PollInterval)
		}
	}
	if len(pending) > 0 {
		for _, route := range pending {
			results = append(results, systemFailure(route, correlation, "CONTRACT-TIMEOUT", "The service did not return a contract result before the fan-in deadline.", fmt.Sprintf("no %s artifact appeared within %s", artifactName(correlation, route.ID), cfg.Timeout)))
		}
	}
	sort.Slice(results, func(i, j int) bool { return results[i].ServiceID < results[j].ServiceID })
	return results, nil
}

func findArtifact(repository, name, token string, started time.Time) (githubArtifact, bool, error) {
	var response struct {
		Artifacts []githubArtifact `json:"artifacts"`
	}
	route := fmt.Sprintf("/repos/%s/actions/artifacts?name=%s&per_page=10", repository, url.QueryEscape(name))
	if err := githubJSON(http.MethodGet, route, token, nil, &response); err != nil {
		return githubArtifact{}, false, err
	}
	for _, artifact := range response.Artifacts {
		if artifact.Name == name && !artifact.Expired && !artifact.CreatedAt.Before(started.Add(-time.Minute)) {
			return artifact, true, nil
		}
	}
	return githubArtifact{}, false, nil
}

func downloadResult(repository string, artifact githubArtifact, token string) (serviceResult, error) {
	var result serviceResult
	request, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("https://api.github.com/repos/%s/actions/artifacts/%d/zip", repository, artifact.ID), nil)
	setGitHubHeaders(request, token)
	response, err := httpClient.Do(request)
	if err != nil {
		return result, err
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(response.Body, 5<<20))
	if response.StatusCode != http.StatusOK {
		return result, fmt.Errorf("artifact download failed (%d): %.400s", response.StatusCode, data)
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return result, err
	}
	for _, file := range reader.File {
		if filepath.Base(file.Name) != "result.json" {
			continue
		}
		opened, err := file.Open()
		if err != nil {
			return result, err
		}
		content, readErr := io.ReadAll(io.LimitReader(opened, 1<<20))
		opened.Close()
		if readErr != nil {
			return result, readErr
		}
		return result, json.Unmarshal(content, &result)
	}
	return result, errors.New("artifact did not contain result.json")
}

func loadLocalResults(routes []serviceRoute, dir, correlation string) ([]serviceResult, error) {
	results := make([]serviceResult, 0, len(routes))
	for _, route := range routes {
		data, err := os.ReadFile(filepath.Join(dir, route.ID+".json"))
		if err != nil {
			return nil, fmt.Errorf("read local result for %s: %w", route.ID, err)
		}
		var result serviceResult
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, err
		}
		if result.CorrelationID == "LOCAL" {
			result.CorrelationID = correlation
		}
		if err := validateResult(result, route, correlation); err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].ServiceID < results[j].ServiceID })
	return results, nil
}

func validateResult(result serviceResult, route serviceRoute, correlation string) error {
	if result.CorrelationID != correlation {
		return fmt.Errorf("service %s returned correlation %q, want %q", route.ID, result.CorrelationID, correlation)
	}
	if result.ServiceID != route.ID {
		return fmt.Errorf("route %s returned service_id %q", route.ID, result.ServiceID)
	}
	if result.Status != "pass" && result.Status != "fail" {
		return fmt.Errorf("service %s returned invalid status %q", route.ID, result.Status)
	}
	if result.Summary == "" || result.Violations == nil {
		return fmt.Errorf("service %s returned an incomplete result", route.ID)
	}
	if result.Status == "pass" && len(result.Violations) != 0 {
		return fmt.Errorf("service %s passed with violations", route.ID)
	}
	if result.Status == "fail" && len(result.Violations) == 0 {
		return fmt.Errorf("service %s failed without violations", route.ID)
	}
	if _, ok := map[string]bool{"none": true, "low": true, "medium": true, "high": true, "critical": true}[result.Risk]; !ok {
		return fmt.Errorf("service %s returned invalid risk %q", route.ID, result.Risk)
	}
	for _, item := range result.Violations {
		if item.RuleID == "" || item.Severity == "" || item.Evidence == "" || item.Reason == "" || item.Remediation == "" {
			return fmt.Errorf("service %s returned an incomplete violation", route.ID)
		}
	}
	return nil
}

func systemFailure(route serviceRoute, correlation, ruleID, summary, evidence string) serviceResult {
	return serviceResult{
		SchemaVersion: "1.0", CorrelationID: correlation, ServiceID: route.ID,
		ServiceName: route.Name, Owner: route.Owner, Repository: route.Repository,
		Status: "fail", Risk: "critical", Summary: summary, Engine: "orchestrator",
		Violations: []violation{{
			RuleID: ruleID, Severity: "critical", Evidence: truncate(evidence, 500),
			Reason:      "The aggregate gate fails closed when a service-owned verdict cannot be obtained and validated.",
			Remediation: "Fix the repository dispatch/service workflow configuration and rerun the infrastructure check.",
		}},
	}
}

func aggregate(results []serviceResult, correlation, repo string, pr int, sha string) aggregateReport {
	failed := 0
	for _, result := range results {
		if result.Status == "fail" {
			failed++
		}
	}
	status := "pass"
	summary := fmt.Sprintf("All %d service repositories accepted this infrastructure change.", len(results))
	if failed > 0 {
		status = "fail"
		summary = fmt.Sprintf("%d of %d service repositories rejected this infrastructure change.", failed, len(results))
	}
	return aggregateReport{SchemaVersion: "1.0", CorrelationID: correlation, InfraRepo: repo, PRNumber: pr, HeadSHA: sha, Status: status, Summary: summary, Services: results}
}

func renderMarkdown(report aggregateReport) string {
	lines := []string{reportMarker, "## Distributed infra-app contract test", "", report.Summary, "", fmt.Sprintf("Correlation: `%s`", report.CorrelationID), "", "| Service repository | Result | Risk | Summary |", "|---|---:|---:|---|"}
	for _, service := range report.Services {
		name := fmt.Sprintf("`%s`", service.Repository)
		if service.WorkflowURL != "" {
			name = fmt.Sprintf("[%s](%s)", service.Repository, service.WorkflowURL)
		}
		lines = append(lines, fmt.Sprintf("| %s | **%s** | %s | %s |", name, strings.ToUpper(service.Status), service.Risk, escapeCell(service.Summary)))
		for _, item := range service.Violations {
			lines = append(lines, fmt.Sprintf("| ↳ `%s` | **%s** | %s | %s |", item.RuleID, strings.ToUpper(service.Status), item.Severity, escapeCell(item.Reason)))
		}
	}
	lines = append(lines, "", "Each verdict was produced inside the owning service repository using that repository's code and manifests plus this infrastructure PR's description and diff.")
	return strings.Join(lines, "\n")
}

func publish(markdown string, report aggregateReport, token string) error {
	if token == "" {
		return errors.New("GITHUB_TOKEN is required to publish the aggregate report")
	}
	repository := os.Getenv("GITHUB_REPOSITORY")
	var comments []struct {
		ID   int64  `json:"id"`
		Body string `json:"body"`
	}
	if err := githubJSON(http.MethodGet, fmt.Sprintf("/repos/%s/issues/%d/comments?per_page=100", repository, report.PRNumber), token, nil, &comments); err != nil {
		return err
	}
	method := http.MethodPost
	route := fmt.Sprintf("/repos/%s/issues/%d/comments", repository, report.PRNumber)
	for _, comment := range comments {
		if strings.Contains(comment.Body, reportMarker) {
			method = http.MethodPatch
			route = fmt.Sprintf("/repos/%s/issues/comments/%d", repository, comment.ID)
			break
		}
	}
	return githubJSON(method, route, token, map[string]string{"body": markdown}, nil)
}

func artifactName(correlation, serviceID string) string {
	return "infra-contract-" + correlation + "-" + serviceID
}

func buildCorrelation(repository string, pr int, sha, runID, attempt string) string {
	short := sha
	if len(short) > 12 {
		short = short[:12]
	}
	value := fmt.Sprintf("%s-pr%d-%s-run%s-%s", strings.ReplaceAll(repository, "/", "-"), pr, short, runID, attempt)
	return sanitize(value)
}

func sanitize(value string) string {
	var builder strings.Builder
	for _, char := range strings.ToLower(value) {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-' {
			builder.WriteRune(char)
		} else {
			builder.WriteByte('-')
		}
	}
	return strings.Trim(builder.String(), "-")
}

func qualifyRepository(repository, sourceRepository string) string {
	if strings.Contains(repository, "/") {
		return repository
	}
	owner, _, _ := strings.Cut(sourceRepository, "/")
	if owner == "" {
		return repository
	}
	return owner + "/" + repository
}

func decodeBase64(value string) ([]byte, error) {
	cleaned := strings.ReplaceAll(strings.ReplaceAll(value, "\n", ""), "\r", "")
	return base64.StdEncoding.DecodeString(cleaned)
}

func githubJSON(method, route, token string, input, output any) error {
	var body io.Reader
	if input != nil {
		encoded, _ := json.Marshal(input)
		body = bytes.NewReader(encoded)
	}
	request, _ := http.NewRequest(method, "https://api.github.com"+route, body)
	request.Header.Set("Accept", "application/vnd.github+json")
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	setGitHubHeaders(request, token)
	response, err := httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("GitHub API %s %s failed (%d): %.400s", method, route, response.StatusCode, data)
	}
	if output != nil && len(data) > 0 {
		return json.Unmarshal(data, output)
	}
	return nil
}

func setGitHubHeaders(request *http.Request, token string) {
	request.Header.Set("User-Agent", "infra-contract-poc")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func envInt(name string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil {
		return fallback
	}
	return value
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func escapeCell(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "|", "\\|"), "\n", " ")
}

package main

import (
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
	"strings"
	"time"
)

type publishConfig struct {
	ContractFile string
	MarkdownFile string
	MarkdownKey  string
	ObservedKey  string
	Target       string
	Observed     []string
}

var client = &http.Client{Timeout: 60 * time.Second}

func main() {
	kind := flag.String("kind", environment("PUBLISH_KIND", ""), "service or platform")
	flag.Parse()
	if err := publish(*kind); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func publish(kind string) error {
	repository := os.Getenv("REGISTRY_REPO")
	token := os.Getenv("REGISTRY_WRITE_TOKEN")
	if repository == "" || token == "" {
		return errors.New("REGISTRY_REPO and REGISTRY_WRITE_TOKEN are required")
	}
	settings, err := settingsFor(kind)
	if err != nil {
		return err
	}
	contractBytes, err := os.ReadFile(settings.ContractFile)
	if err != nil {
		return err
	}
	var document map[string]any
	if err := json.Unmarshal(contractBytes, &document); err != nil {
		return err
	}
	markdown, err := os.ReadFile(settings.MarkdownFile)
	if err != nil {
		return err
	}
	document[settings.MarkdownKey] = string(markdown)
	observed := map[string]string{}
	for _, name := range settings.Observed {
		data, readErr := os.ReadFile(name)
		if readErr != nil {
			return readErr
		}
		observed[name] = string(data)
	}
	document[settings.ObservedKey] = observed
	document["source_revision"] = environment("GITHUB_SHA", "local")
	encoded, _ := json.MarshalIndent(document, "", "  ")
	encoded = append(encoded, '\n')

	branch := environment("REGISTRY_BRANCH", "main")
	current, err := getCurrent(repository, settings.Target, branch, token)
	if err != nil {
		return err
	}
	shortRevision := document["source_revision"].(string)
	if len(shortRevision) > 7 {
		shortRevision = shortRevision[:7]
	}
	body := map[string]any{
		"message": fmt.Sprintf("chore(contracts): publish %s %s", kind, shortRevision),
		"content": base64.StdEncoding.EncodeToString(encoded),
		"branch":  branch,
	}
	if current != "" {
		body["sha"] = current
	}
	if err := githubRequest(http.MethodPut, fmt.Sprintf("/repos/%s/contents/%s", repository, settings.Target), token, body, nil); err != nil {
		return err
	}
	fmt.Printf("Published %s to %s.\n", settings.Target, repository)
	return nil
}

func settingsFor(kind string) (publishConfig, error) {
	switch kind {
	case "service":
		return publishConfig{
			ContractFile: "infra-contract.json", MarkdownFile: "infra-requirements.md",
			MarkdownKey: "requirements_markdown", ObservedKey: "observed_manifests",
			Target:   "contracts/services/search.json",
			Observed: []string{"k8s/deployment.yaml", "k8s/persistent-volume-claim.yaml", "k8s/ingress.yaml"},
		}, nil
	case "platform":
		return publishConfig{
			ContractFile: "platform-contract.json", MarkdownFile: "platform-invariants.md",
			MarkdownKey: "invariants_markdown", ObservedKey: "observed_state",
			Target:   "contracts/platform/shared-platform.json",
			Observed: []string{"infra/storage-class.yaml", "infra/network-state.yaml", "terraform/main.tf"},
		}, nil
	default:
		return publishConfig{}, errors.New("--kind must be service or platform")
	}
}

func getCurrent(repository, target, branch, token string) (string, error) {
	var response struct {
		SHA string `json:"sha"`
	}
	route := fmt.Sprintf("/repos/%s/contents/%s?ref=%s", repository, target, url.QueryEscape(branch))
	err := githubRequest(http.MethodGet, route, token, nil, &response)
	if err != nil && strings.Contains(err.Error(), "(404)") {
		return "", nil
	}
	return response.SHA, err
}

func githubRequest(method, route, token string, input, output any) error {
	var body io.Reader
	if input != nil {
		encoded, _ := json.Marshal(input)
		body = bytes.NewReader(encoded)
	}
	request, _ := http.NewRequest(method, "https://api.github.com"+route, body)
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("User-Agent", "infra-contract-poc")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
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

func environment(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

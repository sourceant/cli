package install

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// AgentRepo publishes the agent binary.
const AgentRepo = "sourceant/agent"

// AgentName is the binary this installs.
const AgentName = "sourceant-agent"

// BinDir is where the agent binary is kept.
func BinDir() string { return filepath.Join(Home(), "bin") }

// AgentPath is the agent this machine runs.
func AgentPath() string { return filepath.Join(BinDir(), AgentName) }

// Platform is the os-arch pair release assets are named for.
func Platform() (string, error) {
	arch := runtime.GOARCH
	switch arch {
	case "amd64", "arm64":
	default:
		return "", fmt.Errorf("no agent is published for %s", arch)
	}
	switch runtime.GOOS {
	case "linux", "darwin":
	default:
		return "", fmt.Errorf("no agent is published for %s", runtime.GOOS)
	}
	return runtime.GOOS + "-" + arch, nil
}

// Fetcher gets a URL. Swapped in tests, because an install that really reaches
// GitHub is not something to run on every change.
type Fetcher func(url string) (io.ReadCloser, error)

// Get fetches over HTTP.
func Get(url string) (io.ReadCloser, error) {
	client := &http.Client{Timeout: 5 * time.Minute}
	response, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("%s answered %s", url, response.Status)
	}
	return response.Body, nil
}

// LatestAgent asks which version to install when none was named.
func LatestAgent(get Fetcher) (string, error) {
	body, err := get("https://api.github.com/repos/" + AgentRepo + "/releases/latest")
	if err != nil {
		return "", err
	}
	defer body.Close()
	var release struct {
		Tag string `json:"tag_name"`
	}
	if err := json.NewDecoder(body).Decode(&release); err != nil {
		return "", err
	}
	tag := strings.TrimPrefix(release.Tag, "v")
	if tag == "" {
		return "", fmt.Errorf("%s names no latest release", AgentRepo)
	}
	return tag, nil
}

// AgentURL is where one version's asset lives.
func AgentURL(version, platform string) string {
	asset := fmt.Sprintf("%s-%s-%s.tar.gz", AgentName, version, platform)
	return fmt.Sprintf("https://github.com/%s/releases/download/v%s/%s", AgentRepo, version, asset)
}

// InstallAgent puts the agent beside the CLI and returns where it went. The
// archive holds one file named for the version and platform, so what comes out
// is renamed to something a person can type.
func InstallAgent(version string, get Fetcher, out io.Writer) (string, error) {
	platform, err := Platform()
	if err != nil {
		return "", err
	}
	if version == "" || version == "dev" || version == "latest" {
		version, err = LatestAgent(get)
		if err != nil {
			return "", fmt.Errorf("could not tell which agent to install: %w", err)
		}
	}

	say(out, "Fetching %s %s for %s.\n", AgentName, version, platform)
	body, err := get(AgentURL(version, platform))
	if err != nil {
		return "", fmt.Errorf("could not fetch the agent: %w", err)
	}
	defer body.Close()

	if err := os.MkdirAll(BinDir(), 0o755); err != nil {
		return "", err
	}
	path := AgentPath()
	if err := extractOne(body, path); err != nil {
		return "", err
	}
	return path, nil
}

// extractOne writes the first regular file in a gzipped tar to path.
func extractOne(archive io.Reader, path string) error {
	zipped, err := gzip.NewReader(archive)
	if err != nil {
		return fmt.Errorf("the agent archive is not gzip: %w", err)
	}
	defer zipped.Close()

	reader := tar.NewReader(zipped)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return fmt.Errorf("the agent archive held no file")
		}
		if err != nil {
			return err
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return err
		}
		defer file.Close()
		if _, err := io.Copy(file, reader); err != nil {
			return err
		}
		return file.Chmod(0o755)
	}
}

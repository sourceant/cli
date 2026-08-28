// Package install puts a SourceAnt core on this machine and writes down which
// one, so the agent can start it.
//
// The file written here is read by the agent. Its field names are the contract
// between the two: change one and change the other, or the agent will start
// nothing. TestTheFileMatchesWhatTheAgentReads pins the shape.
package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// Runtime is how the core is installed.
type Runtime string

const (
	Python Runtime = "python"
	Docker Runtime = "docker"
)

// DefaultImage is the published core.
const DefaultImage = "ghcr.io/sourceant/sourceant:latest"

// DefaultPackage is the core on PyPI.
const DefaultPackage = "sourceant"

// Core is what was installed.
type Core struct {
	Runtime Runtime `json:"runtime"`
	Command string  `json:"command,omitempty"`
	Image   string  `json:"image,omitempty"`
	DataDir string  `json:"data_dir,omitempty"`
	Mount   string  `json:"mount,omitempty"`
	User    string  `json:"user,omitempty"`
}

// Config is the whole file.
type Config struct {
	Core Core `json:"core"`
}

// Home is where SourceAnt keeps what it installed.
func Home() string {
	if override := os.Getenv("SOURCEANT_INSTALL_HOME"); override != "" {
		return override
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".sourceant"
	}
	return filepath.Join(home, ".sourceant")
}

// ConfigPath is the file the agent reads.
func ConfigPath() string { return filepath.Join(Home(), "config.json") }

// DataDir is where the index lives, matching what the core picks for itself.
//
// Both runtimes have to agree on it. If they did not, indexing and reading
// would address two different databases and the second would look empty.
func DataDir() string {
	if override := os.Getenv("SOURCEANT_HOME"); override != "" {
		return override
	}
	if base := os.Getenv("XDG_DATA_HOME"); base != "" {
		return filepath.Join(base, "sourceant")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".sourceant-data"
	}
	return filepath.Join(home, ".local", "share", "sourceant")
}

// Save writes the runtime for the agent to read.
func Save(path string, config Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// Options describe what to install.
type Options struct {
	Runtime Runtime
	// Image is the container to pull, for the docker runtime.
	Image string
	// From is what pip installs, for the python runtime. A package name, a
	// version specifier, or a path to a checkout.
	From string
	// Pull says whether to fetch the image before writing anything down.
	Pull bool
	// Out receives progress.
	Out io.Writer
}

// Runner runs the commands an install needs. Swapped in tests, because an
// install that really pulls an image is not something to run on every change.
type Runner func(name string, args ...string) ([]byte, error)

// Run executes a command and returns its combined output.
func Run(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

// Install puts the core on this machine and returns what to write down.
func Install(opts Options, run Runner) (Config, error) {
	switch opts.Runtime {
	case Docker:
		return installDocker(opts, run)
	case Python:
		return installPython(opts, run)
	default:
		return Config{}, fmt.Errorf("runtime %q is neither python nor docker", opts.Runtime)
	}
}

func installDocker(opts Options, run Runner) (Config, error) {
	image := opts.Image
	if image == "" {
		image = DefaultImage
	}
	if _, err := run("docker", "version", "--format", "{{.Server.Version}}"); err != nil {
		return Config{}, errors.New("docker is not running here. Start it, or install the python runtime instead")
	}
	if opts.Pull {
		say(opts.Out, "Pulling %s. This is about 1.4 GB the first time.\n", image)
		if output, err := run("docker", "pull", image); err != nil {
			return Config{}, fmt.Errorf("could not pull %s: %s", image, trim(output))
		}
	}
	if _, err := run("docker", "image", "inspect", image); err != nil {
		return Config{}, fmt.Errorf("%s is not on this machine. Run without --no-pull to fetch it", image)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, fmt.Errorf("could not find your home directory: %w", err)
	}

	return Config{Core: Core{
		Runtime: Docker,
		Image:   image,
		DataDir: DataDir(),
		// The indexer reads a repository's files, so the container has to be
		// able to see them. Your home is mounted at the path it already has,
		// which is what lets one registry of absolute paths mean the same
		// thing to either runtime. A repository outside it is not readable
		// this way.
		Mount: home,
		// The image has a user of its own, and where its id differs from this
		// person's, everything written into the mounted index would belong to
		// somebody who does not exist here.
		User: fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
	}}, nil
}

func installPython(opts Options, run Runner) (Config, error) {
	from := opts.From
	if from == "" {
		from = DefaultPackage
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		return Config{}, errors.New("no python3 on PATH. Install one, or install the docker runtime instead")
	}

	venv := filepath.Join(Home(), "runtime")
	say(opts.Out, "Building a runtime in %s\n", venv)
	if output, err := run(python, "-m", "venv", venv); err != nil {
		return Config{}, fmt.Errorf("could not build a runtime: %s", trim(output))
	}

	pip := filepath.Join(venv, "bin", "pip")
	say(opts.Out, "Installing %s\n", from)
	if output, err := run(pip, "install", "--upgrade", from); err != nil {
		return Config{}, fmt.Errorf("could not install %s: %s", from, trim(output))
	}

	command := filepath.Join(venv, "bin", "sourceant")
	if _, err := os.Stat(command); err != nil {
		return Config{}, fmt.Errorf(
			"%s installed without a sourceant command, so there is nothing to start. "+
				"The core is not packaged for PyPI yet; use --runtime docker", from)
	}

	return Config{Core: Core{
		Runtime: Python,
		Command: command,
		DataDir: DataDir(),
	}}, nil
}

// Describe is one line naming what was installed.
func (c Core) Describe() string {
	switch c.Runtime {
	case Python:
		return "python · " + c.Command
	case Docker:
		return "docker · " + c.Image + " as " + c.User
	default:
		return string(c.Runtime)
	}
}

func say(out io.Writer, format string, args ...any) {
	if out != nil {
		_, _ = fmt.Fprintf(out, format, args...)
	}
}

// trim keeps a failure readable: the last of a command's output is where it
// says what went wrong, and the rest is progress nobody needs now.
func trim(output []byte) string {
	const keep = 400
	text := string(output)
	if len(text) <= keep {
		return text
	}
	return "…" + text[len(text)-keep:]
}

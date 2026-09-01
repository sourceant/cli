package install

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
)

// archive builds what a release asset actually is: one file named for the
// version and platform, gzipped inside a tar.
func archive(t *testing.T, name, body string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	zipped := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(zipped)
	header := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}
	if err := writer.WriteHeader(header); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	writer.Close()
	zipped.Close()
	return buffer.Bytes()
}

func TestItInstallsTheAgentNamedSomethingAPersonCanType(t *testing.T) {
	t.Setenv("SOURCEANT_INSTALL_HOME", t.TempDir())
	platform, err := Platform()
	if err != nil {
		t.Skip(err)
	}
	asset := fmt.Sprintf("sourceant-agent-1.2.3-%s.tar.gz", platform)
	var asked string
	get := func(url string) (io.ReadCloser, error) {
		asked = url
		return io.NopCloser(bytes.NewReader(archive(t, strings.TrimSuffix(asset, ".tar.gz"), "binary"))), nil
	}

	path, err := InstallAgent("1.2.3", get, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasSuffix(asked, asset) {
		t.Errorf("fetched %q, want it to end in %q", asked, asset)
	}
	if got := path; !strings.HasSuffix(got, "/bin/sourceant-agent") {
		t.Errorf("installed to %q, want a typeable name", got)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "binary" {
		t.Errorf("wrote %q, want the archived file", body)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("mode %v, want it executable", info.Mode().Perm())
	}
}

func TestItAsksWhichVersionWhenTheBuildDoesNotKnow(t *testing.T) {
	t.Setenv("SOURCEANT_INSTALL_HOME", t.TempDir())
	platform, err := Platform()
	if err != nil {
		t.Skip(err)
	}
	get := func(url string) (io.ReadCloser, error) {
		if strings.Contains(url, "api.github.com") {
			return io.NopCloser(strings.NewReader(`{"tag_name":"v9.9.9"}`)), nil
		}
		if !strings.Contains(url, "v9.9.9") {
			t.Errorf("fetched %q, want the version the API named", url)
		}
		name := fmt.Sprintf("sourceant-agent-9.9.9-%s", platform)
		return io.NopCloser(bytes.NewReader(archive(t, name, "binary"))), nil
	}

	if _, err := InstallAgent("dev", get, io.Discard); err != nil {
		t.Fatal(err)
	}
}

func TestItSaysSoWhenTheAgentCannotBeFetched(t *testing.T) {
	t.Setenv("SOURCEANT_INSTALL_HOME", t.TempDir())
	if _, err := Platform(); err != nil {
		t.Skip(err)
	}
	get := func(string) (io.ReadCloser, error) {
		return nil, fmt.Errorf("404 Not Found")
	}

	_, err := InstallAgent("1.2.3", get, io.Discard)

	if err == nil {
		t.Fatal("installed something from a fetch that failed")
	}
	if !strings.Contains(err.Error(), "could not fetch the agent") {
		t.Errorf("got %q, want it to say what failed", err)
	}
}

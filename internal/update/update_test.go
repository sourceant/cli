package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func archive(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var packed bytes.Buffer
	zipped := gzip.NewWriter(&packed)
	writer := tar.NewWriter(zipped)
	if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zipped.Close(); err != nil {
		t.Fatal(err)
	}
	return packed.Bytes()
}

func sum(body []byte) string {
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}

func TestAnArchiveThatMatchesItsChecksumGivesUpTheBinary(t *testing.T) {
	packed := archive(t, "sourceant-1.0.0-linux-amd64", []byte("the binary"))
	checksums := sum(packed) + "  sourceant-1.0.0-linux-amd64.tar.gz\n"
	get := func(_ context.Context, url string) ([]byte, error) {
		if url == "archive" {
			return packed, nil
		}
		return []byte(checksums), nil
	}
	release := Release{Tag: "v1.0.0", Assets: []Asset{
		{Name: "sourceant-1.0.0-linux-amd64.tar.gz", URL: "archive"},
		{Name: "checksums.txt", URL: "sums"},
	}}

	binary, err := Binary(context.Background(), get, release, "sourceant-1.0.0-linux-amd64.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if string(binary) != "the binary" {
		t.Errorf("got %q", binary)
	}
}

func TestAnArchiveThatDoesNotMatchIsRefused(t *testing.T) {
	packed := archive(t, "sourceant-1.0.0-linux-amd64", []byte("the binary"))
	get := func(_ context.Context, url string) ([]byte, error) {
		if url == "archive" {
			return packed, nil
		}
		return []byte(sum([]byte("something else")) + "  sourceant-1.0.0-linux-amd64.tar.gz\n"), nil
	}
	release := Release{Tag: "v1.0.0", Assets: []Asset{
		{Name: "sourceant-1.0.0-linux-amd64.tar.gz", URL: "archive"},
		{Name: "checksums.txt", URL: "sums"},
	}}

	if _, err := Binary(context.Background(), get, release, "sourceant-1.0.0-linux-amd64.tar.gz"); err == nil {
		t.Fatal("took an archive whose checksum did not match")
	}
}

func TestAReleaseWithoutChecksumsIsRefused(t *testing.T) {
	release := Release{Tag: "v1.0.0", Assets: []Asset{{Name: "sourceant-1.0.0-linux-amd64.tar.gz", URL: "archive"}}}
	get := func(context.Context, string) ([]byte, error) { return nil, nil }

	if _, err := Binary(context.Background(), get, release, "sourceant-1.0.0-linux-amd64.tar.gz"); err == nil {
		t.Fatal("took a release that publishes no checksums")
	}
}

func TestReplacingKeepsTheOldFileUntilTheNewOneIsWhole(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sourceant")
	if err := os.WriteFile(path, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := Replace(path, []byte("new")); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "new" {
		t.Errorf("got %q", body)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("mode is %v", info.Mode().Perm())
	}
	left, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".sourceant-update-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Errorf("left %v behind", left)
	}
}

func TestOneVersionIsNewerThanAnother(t *testing.T) {
	for _, item := range []struct {
		candidate, current string
		want               bool
	}{
		{"1.0.1", "1.0.0", true},
		{"1.0.0", "1.0.0", false},
		{"1.0.0", "1.0.1", false},
		{"1.0.0", "1.0.0-beta.5", true},
		{"1.0.0-beta.5", "1.0.0-beta.4", true},
		{"1.0.0-beta.4", "1.0.0-beta.5", false},
		{"1.0.0-beta.10", "1.0.0-beta.9", true},
	} {
		got, err := Newer(item.candidate, item.current)
		if err != nil {
			t.Fatalf("%s vs %s: %v", item.candidate, item.current, err)
		}
		if got != item.want {
			t.Errorf("%s newer than %s: got %v", item.candidate, item.current, got)
		}
	}
}

func TestAPrereleaseIsSkippedUnlessAskedFor(t *testing.T) {
	t.Setenv("SOURCEANT_RELEASES_BASE", "https://example.invalid")
	asked := ""
	get := func(_ context.Context, url string) ([]byte, error) {
		asked = url
		if url == "https://example.invalid/sourceant/cli/releases/latest" {
			return []byte(`{"tag_name":"v0.9.0"}`), nil
		}
		return []byte(`[{"tag_name":"v1.0.0-beta.4","prerelease":true}]`), nil
	}

	stable, err := Latest(context.Background(), get, "sourceant/cli", false)
	if err != nil {
		t.Fatal(err)
	}
	if stable.Version() != "0.9.0" {
		t.Errorf("got %s from %s", stable.Version(), asked)
	}

	early, err := Latest(context.Background(), get, "sourceant/cli", true)
	if err != nil {
		t.Fatal(err)
	}
	if early.Version() != "1.0.0-beta.4" {
		t.Errorf("got %s from %s", early.Version(), asked)
	}
}

// Package update replaces what is installed with what a release holds.
//
// Every download is checked against the release's own checksums before it is
// written anywhere, and a replacement is renamed over the old file rather than
// written through it, so an interrupted update leaves the old one working.
package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Largest thing worth fetching. An agent archive is about 10 MiB.
const most = 100 << 20

// Release is one published release and what it carries.
type Release struct {
	Tag        string  `json:"tag_name"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
}

// Version is the release's version, without the leading v.
func (r Release) Version() string { return strings.TrimPrefix(r.Tag, "v") }

// Asset is one file on a release.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

// Fetcher reads a URL.
type Fetcher func(ctx context.Context, url string) ([]byte, error)

// Latest is the newest release of a repository. Prereleases are included when
// asked for, which is what a machine already running one wants.
func Latest(ctx context.Context, get Fetcher, repo string, prerelease bool) (Release, error) {
	base := api(repo)
	if !prerelease {
		body, err := get(ctx, base+"/latest")
		if err != nil {
			return Release{}, err
		}
		var found Release
		if err := json.Unmarshal(body, &found); err != nil {
			return Release{}, fmt.Errorf("%s answered with something other than a release: %w", repo, err)
		}
		return found, nil
	}
	body, err := get(ctx, base+"?per_page=20")
	if err != nil {
		return Release{}, err
	}
	var found []Release
	if err := json.Unmarshal(body, &found); err != nil {
		return Release{}, fmt.Errorf("%s answered with something other than releases: %w", repo, err)
	}
	for _, release := range found {
		if !release.Draft {
			return release, nil
		}
	}
	return Release{}, fmt.Errorf("%s has published no release", repo)
}

// Named is one release by version, whether or not it is the latest.
func Named(ctx context.Context, get Fetcher, repo, version string) (Release, error) {
	body, err := get(ctx, api(repo)+"/tags/v"+strings.TrimPrefix(version, "v"))
	if err != nil {
		return Release{}, fmt.Errorf("%s has no release %s: %w", repo, version, err)
	}
	var found Release
	if err := json.Unmarshal(body, &found); err != nil {
		return Release{}, err
	}
	return found, nil
}

// Binary is the verified binary held in one of a release's archives.
func Binary(ctx context.Context, get Fetcher, release Release, archive string) ([]byte, error) {
	asset, ok := find(release.Assets, archive)
	if !ok {
		return nil, fmt.Errorf("release %s does not carry %s", release.Version(), archive)
	}
	sums, ok := find(release.Assets, "checksums.txt")
	if !ok {
		return nil, fmt.Errorf("release %s publishes no checksums", release.Version())
	}
	body, err := get(ctx, asset.URL)
	if err != nil {
		return nil, err
	}
	listed, err := get(ctx, sums.URL)
	if err != nil {
		return nil, err
	}
	if err := verify(archive, body, string(listed)); err != nil {
		return nil, err
	}
	return first(body)
}

// Replace puts binary where path is, by rename, keeping the old file's mode.
func Replace(path string, binary []byte) error {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	mode := os.FileMode(0o755)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".sourceant-update-*")
	if err != nil {
		return fmt.Errorf("could not write beside %s: %w", path, err)
	}
	name := temporary.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err := temporary.Write(binary); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// Newer says whether candidate is a later version than current.
func Newer(candidate, current string) (bool, error) {
	later, err := parse(candidate)
	if err != nil {
		return false, err
	}
	now, err := parse(current)
	if err != nil {
		return false, err
	}
	for i := 0; i < 3; i++ {
		if later.numbers[i] != now.numbers[i] {
			return later.numbers[i] > now.numbers[i], nil
		}
	}
	switch {
	case later.pre == now.pre:
		return false, nil
	case later.pre == "":
		return true, nil
	case now.pre == "":
		return false, nil
	}
	return compare(later.pre, now.pre) > 0, nil
}

// Read fetches a URL, capped, with the headers the forge asks for.
func Read(ctx context.Context, url string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "sourceant-cli")
	answer, err := (&http.Client{Timeout: 2 * time.Minute}).Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = answer.Body.Close() }()
	if answer.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", url, answer.Status)
	}
	body, err := io.ReadAll(io.LimitReader(answer.Body, most+1))
	if err != nil {
		return nil, err
	}
	if len(body) > most {
		return nil, fmt.Errorf("%s is larger than %d bytes", url, most)
	}
	return body, nil
}

func api(repo string) string {
	if base := os.Getenv("SOURCEANT_RELEASES_BASE"); base != "" {
		return strings.TrimRight(base, "/") + "/" + repo + "/releases"
	}
	return "https://api.github.com/repos/" + repo + "/releases"
}

func find(assets []Asset, name string) (Asset, bool) {
	for _, asset := range assets {
		if asset.Name == name {
			return asset, true
		}
	}
	return Asset{}, false
}

func verify(name string, body []byte, checksums string) error {
	want := ""
	for _, line := range strings.Split(checksums, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			want = fields[0]
			break
		}
	}
	if want == "" {
		return fmt.Errorf("the checksums do not cover %s", name)
	}
	sum := sha256.Sum256(body)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), want) {
		return fmt.Errorf("%s does not match its checksum", name)
	}
	return nil
}

// first is the first regular file in a gzipped tar, which is how both archives
// carry their one binary.
func first(archive []byte) ([]byte, error) {
	zipped, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("the archive is not gzip: %w", err)
	}
	defer func() { _ = zipped.Close() }()
	reader := tar.NewReader(zipped)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil, errors.New("the archive holds no file")
		}
		if err != nil {
			return nil, err
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		if header.Size < 0 || header.Size > most {
			return nil, errors.New("the file in the archive is too large")
		}
		body, err := io.ReadAll(io.LimitReader(reader, header.Size))
		if err != nil {
			return nil, err
		}
		if int64(len(body)) != header.Size {
			return nil, errors.New("the file in the archive is truncated")
		}
		return body, nil
	}
}

type parts struct {
	numbers [3]int
	pre     string
}

func parse(value string) (parts, error) {
	value = strings.TrimPrefix(value, "v")
	value = strings.SplitN(value, "+", 2)[0]
	split := strings.SplitN(value, "-", 2)
	numbers := strings.Split(split[0], ".")
	if len(numbers) != 3 {
		return parts{}, fmt.Errorf("%q is not a version", value)
	}
	var found parts
	for i, number := range numbers {
		parsed, err := strconv.Atoi(number)
		if err != nil || parsed < 0 {
			return parts{}, fmt.Errorf("%q is not a version", value)
		}
		found.numbers[i] = parsed
	}
	if len(split) == 2 {
		found.pre = split[1]
	}
	return found, nil
}

func compare(left, right string) int {
	l := strings.FieldsFunc(left, func(r rune) bool { return r == '.' || r == '-' })
	r := strings.FieldsFunc(right, func(r rune) bool { return r == '.' || r == '-' })
	for i := 0; i < len(l) && i < len(r); i++ {
		ln, le := strconv.Atoi(l[i])
		rn, re := strconv.Atoi(r[i])
		if le == nil && re == nil && ln != rn {
			if ln > rn {
				return 1
			}
			return -1
		}
		if l[i] != r[i] {
			if le == nil {
				return -1
			}
			if re == nil || l[i] > r[i] {
				return 1
			}
			return -1
		}
	}
	switch {
	case len(l) > len(r):
		return 1
	case len(l) < len(r):
		return -1
	}
	return 0
}

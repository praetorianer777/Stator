package example

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"slices"
)

// Release is one version of the content, with what it brings an example
// made from an older one, a line each, in every language.
type Release struct {
	Version int `json:"version"`
	// Digest is Digest() of the content this version shipped; a test holds
	// the latest to the content, so changing a page asks for a new version.
	Digest string              `json:"digest"`
	New    map[string][]string `json:"new"`
}

const versionsFile = "content/versions.json"

var releases = mustReleases()

func mustReleases() []Release {
	out, err := Releases()
	if err != nil {
		panic(err)
	}
	return out
}

// Releases reads every version of the content, oldest first.
func Releases() ([]Release, error) {
	src, err := files.ReadFile(versionsFile)
	if err != nil {
		return nil, err
	}
	var out []Release
	if err := json.Unmarshal(src, &out); err != nil {
		return nil, fmt.Errorf("%s: %w", versionsFile, err)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s names no version", versionsFile)
	}
	return out, nil
}

// Current is the version of the content an example is made or renewed from.
func Current() int { return releases[len(releases)-1].Version }

// NewSince is what the versions after from bring, oldest first, in lang.
func NewSince(from int, lang string) []string {
	out := []string{}
	for _, r := range releases {
		if r.Version > from {
			out = append(out, r.New[lang]...)
		}
	}
	return out
}

// Digest sums every page of the content in every language, the file of
// versions aside.
func Digest() (string, error) {
	var names []string
	err := fs.WalkDir(files, "content", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || name == versionsFile {
			return err
		}
		names = append(names, name)
		return nil
	})
	if err != nil {
		return "", err
	}
	slices.Sort(names)
	sum := sha256.New()
	for _, name := range names {
		src, err := files.ReadFile(name)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(sum, "%s %d\n", name, len(src))
		sum.Write(src)
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

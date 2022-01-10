// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/spf13/cobra"
)

var (
	githubRegexp    = regexp.MustCompile("^(https?://)?github.com/([^/]+)/([^/]+)/blob/([^/]+)/(.+)$")
	gitlabRegexp    = regexp.MustCompile("^(https?://)?([^/]+)/([^/]+)/([^/]+)/-/blob/([^/]+)/(.+)$")
	bitbucketRegexp = regexp.MustCompile("^(https?://)?bitbucket.org/([^/]+)/([^/]+)/src/([^/]+)/(.+)$")
)

func CheckoutCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "checkout <url to entrypoint>",
		Short:        "Checkout a repository or example",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		Hidden:       true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			url := args[0]

			if _, err := checkout(cmd.Context(), url, cwd); err != nil {
				return err
			}

			return nil
		},
	}

	return cmd
}

func checkout(ctx context.Context, url string, cwd string) (string, error) {
	proj, err := parseURL(url)
	if err != nil {
		return "", err
	}

	name := proj.Name()
	repoPath := filepath.Join(cwd, name)
	fmt.Printf("Downloading %s into %s\n", proj.URL, name)
	_, err = git.PlainCloneContext(ctx, repoPath, false, &git.CloneOptions{
		URL:           proj.URL,
		ReferenceName: plumbing.NewBranchReferenceName(proj.Branch),
		Progress:      os.Stdout,
	})
	if err != nil {
		return "", fmt.Errorf("failed to clone repository, reason: %w", err)
	}

	jag, err := os.Executable()
	if err != nil {
		os.RemoveAll(repoPath)
		return "", err
	}

	exampleDirectory := filepath.Join(repoPath, path.Dir(proj.File))
	relDirectory := exampleDirectory
	if dir, err := filepath.Rel(cwd, exampleDirectory); err == nil {
		relDirectory = dir
	}
	fmt.Println("Installing toit dependencies in", relDirectory)
	cmd := exec.CommandContext(ctx, jag, "pkg", "install")
	cmd.Dir = exampleDirectory
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		os.RemoveAll(repoPath)
		return "", err
	}

	return exampleDirectory, nil
}

type Repository struct {
	Project string
	File    string
	URL     string
	Branch  string
}

func (r *Repository) Name() string {
	return strings.TrimSuffix(path.Base(r.File), path.Ext(r.File))
}

func parseURL(url string) (*Repository, error) {
	if githubRegexp.MatchString(url) {
		return parseGithub(url)
	}
	if bitbucketRegexp.MatchString(url) {
		return parseBitbucket(url)
	}

	if gitlabRegexp.MatchString(url) {
		return parseGitlab(url)
	}

	return nil, fmt.Errorf("Could not parse the repository URL: '%s'", url)
}

func parseGithub(url string) (*Repository, error) {
	matches := githubRegexp.FindStringSubmatch(url)

	if len(matches) != 6 {
		return nil, fmt.Errorf("URL: '%s' was not a github path", url)
	}

	return &Repository{
		File:    matches[5],
		Project: matches[3],
		URL:     fmt.Sprintf("https://github.com/%s/%s", matches[2], matches[3]),
		Branch:  matches[4],
	}, nil
}

func parseGitlab(url string) (*Repository, error) {
	matches := gitlabRegexp.FindStringSubmatch(url)

	if len(matches) != 7 {
		return nil, fmt.Errorf("URL: '%s' was not a gitlab path", url)
	}

	return &Repository{
		File:    matches[6],
		Project: matches[4],
		URL:     fmt.Sprintf("https://%s/%s/%s", matches[2], matches[3], matches[4]),
		Branch:  matches[5],
	}, nil
}

func parseBitbucket(url string) (*Repository, error) {
	matches := bitbucketRegexp.FindStringSubmatch(url)

	if len(matches) != 6 {
		return nil, fmt.Errorf("URL: '%s' was not a bitbucket path", url)
	}

	return &Repository{
		File:    matches[5],
		Project: matches[3],
		URL:     fmt.Sprintf("https://bitbucket.org/%s/%s", matches[2], matches[3]),
		Branch:  matches[4],
	}, nil
}

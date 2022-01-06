// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

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
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			url := args[0]
			if _, err := checkout(url, cwd); err != nil {
				return err
			}
			return nil
		},
	}

	return cmd
}

func checkout(url string, cwd string) (string, error) {
	proj, err := parseURL(url)
	if err != nil {
		return "", err
	}

	path := filepath.Join(cwd, proj.Project)
	fmt.Printf("Downloading %s into %s\n", proj.URL, proj.Project)
	_, err = git.PlainClone(path, false, &git.CloneOptions{
		URL:           proj.URL,
		ReferenceName: plumbing.NewBranchReferenceName(proj.Branch),
		Progress:      os.Stdout,
	})
	if err != nil {
		return "", fmt.Errorf("failed to clone repository, reason: %w", err)
	}

	return proj.Project, nil
}

type Repository struct {
	Project string
	File    string
	URL     string
	Branch  string
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

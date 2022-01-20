package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_parseGithub(t *testing.T) {
	tests := []struct {
		in   string
		repo *Repository
	}{
		{in: "foo"},
		{
			in: "https://github.com/toitlang/toit/blob/master/examples/hello.toit",
			repo: &Repository{
				File:    "examples/hello.toit",
				Project: "toit",
				URL:     "https://github.com/toitlang/toit",
				Branch:  "master",
			},
		},
		{
			in: "github.com/toitlang/toit/blob/foo/examples/hello.toit",
			repo: &Repository{
				File:    "examples/hello.toit",
				Project: "toit",
				URL:     "https://github.com/toitlang/toit",
				Branch:  "foo",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.in, func(t *testing.T) {
			res, err := parseGithub(test.in)
			if test.repo == nil {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, test.repo, res)
			}
		})
	}
}

func Test_parseGitlab(t *testing.T) {
	tests := []struct {
		in   string
		repo *Repository
	}{
		{in: "foo"},
		{
			in: "https://gitlab.matrix.org/matrix-org/olm/-/blob/wip-clang-format/lib/curve25519-donna.h",
			repo: &Repository{
				File:    "lib/curve25519-donna.h",
				Project: "olm",
				URL:     "https://gitlab.matrix.org/matrix-org/olm",
				Branch:  "wip-clang-format",
			},
		},
		{
			in: "https://gitlab.com/gitlab-org/gitlab/-/blob/master/.eslintrc.yml",
			repo: &Repository{
				File:    ".eslintrc.yml",
				Project: "gitlab",
				URL:     "https://gitlab.com/gitlab-org/gitlab",
				Branch:  "master",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.in, func(t *testing.T) {
			res, err := parseGitlab(test.in)
			if test.repo == nil {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, test.repo, res)
			}
		})
	}
}

func Test_parseBitbucket(t *testing.T) {
	tests := []struct {
		in   string
		repo *Repository
	}{
		{in: "foo"},
		{
			in: "https://bitbucket.org/toitlang/toit/src/master/examples/hello.toit",
			repo: &Repository{
				File:    "examples/hello.toit",
				Project: "toit",
				URL:     "https://bitbucket.org/toitlang/toit",
				Branch:  "master",
			},
		},
		{
			in: "bitbucket.org/toitlang/toit/src/foo/examples/hello.toit",
			repo: &Repository{
				File:    "examples/hello.toit",
				Project: "toit",
				URL:     "https://bitbucket.org/toitlang/toit",
				Branch:  "foo",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.in, func(t *testing.T) {
			res, err := parseBitbucket(test.in)
			if test.repo == nil {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, test.repo, res)
			}
		})
	}
}

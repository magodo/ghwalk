package ghwalk

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var githubToken string

func TestMain(m *testing.M) {
	githubToken = os.Getenv("GHWALK_GITHUB_TOKEN")
	if githubToken == "" {
		log.Fatal(`"GHWALK_GITHUB_TOKEN" is not specified`)
	}
	os.Exit(m.Run())
}

func TestWalk(t *testing.T) {
	cases := []struct {
		owner      string
		repo       string
		path       string
		expectPath []string
		skipError  bool
		isError    bool
	}{
		{
			owner: "magodo",
			repo:  "ghwalk",
			path:  "testdata",
			expectPath: []string{
				"testdata",
				"testdata/a",
				"testdata/b",
				"testdata/dir",
				"testdata/dir/c",
				"testdata/link_dir",
			},
		},
		{
			owner:   "magodo",
			repo:    "ghwalk",
			path:    "testdata/non_existent",
			isError: true,
		},
		{
			owner:      "magodo",
			repo:       "ghwalk",
			path:       "testdata/non_existent",
			skipError:  true,
			expectPath: []string{},
		},
	}

	for _, c := range cases {
		traversedPath := []string{}
		ctx, _ := context.WithTimeout(context.Background(), 20*time.Second)
		err := Walk(ctx,
			c.owner, c.repo, c.path,
			&WalkOptions{Token: githubToken},
			func(path string, info *FileInfo, err error) error {
				if err != nil {
					if c.skipError {
						return SkipDir
					}
					return err
				}

				// skip repo root since no info is got
				if info == nil {
					return nil
				}

				traversedPath = append(traversedPath, path)
				return nil
			})
		if c.isError {
			require.Error(t, err)
			continue
		}
		require.NoError(t, err)
		require.Equal(t, c.expectPath, traversedPath)
	}
}

func TestWalkWithFileOnlyInfo(t *testing.T) {
	cases := []struct {
		owner      string
		repo       string
		path       string
		expectPath []string
	}{
		{
			owner: "magodo",
			repo:  "ghwalk",
			path:  "testdata",
			expectPath: []string{
				"testdata",
				"testdata/a",
				"testdata/b",
				"testdata/dir",
				"testdata/dir/c",
				"testdata/link_dir",
			},
		},
	}

	for _, c := range cases {
		traversedPath := []string{}
		ctx, _ := context.WithTimeout(context.Background(), 20*time.Second)
		err := Walk(ctx,
			c.owner, c.repo, c.path,
			&WalkOptions{Token: githubToken, EnableFileOnlyInfo: true},
			func(path string, info *FileInfo, err error) error {
				if err != nil {
					return err
				}

				// skip repo root since no info is got
				if info == nil {
					return nil
				}

				traversedPath = append(traversedPath, path)
				return nil
			})
		require.NoError(t, err)
		require.Equal(t, c.expectPath, traversedPath)
	}
}

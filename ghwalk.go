package ghwalk

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"

	"github.com/google/go-github/v32/github"
	"golang.org/x/oauth2"
)

// SkipDir is used as a return value from WalkFuncs to indicate that
// the directory named in the call is to be skipped. It is not returned
// as an error by any function.
var SkipDir = errors.New("skip this directory")

type WalkOptions struct {
	// Github oauth2 access token
	Token string

	// Github git ref, can be a SHA, branch or a tag
	Ref string
}

type FileInfo struct {
	raw github.RepositoryContent

	Type string

	// Target is only set if the type is "symlink" and the target is not a normal file.
	// If Target is set, Path will be the symlink path.
	Target *string

	// Only available for file type
	Encoding *string

	Size int
	Name string
	Path string

	// Content contains the actual file content, which may be encoded.
	// Callers should call GetContent which will decode the content if
	// necessary.
	//
	// Only available for file type
	Content *string

	SHA     string
	URL     string
	GitURL  string
	HTMLURL string

	// Only available for file type
	DownloadURL *string
}

func (f *FileInfo) GetContent() (string, error) {
	return f.raw.GetContent()
}

func (f *FileInfo) IsDir() bool {
	return f.Type == "dir"
}

// WalkFunc is the type of the function called for each file or directory
// visited by Walk. The path argument contains the argument to Walk as a
// prefix; that is, if Walk is called with "dir", which is a directory
// containing the file "a", the walk function will be called with argument
// "dir/a". The info argument is the FileInfo for the named path.
//
// If there was a problem walking to the file or directory named by path, the
// incoming error will describe the problem and the function can decide how
// to handle that error (and Walk will not descend into that directory). In the
// case of an error, the info argument will be nil. If an error is returned,
// processing stops. The sole exception is when the function returns the special
// value SkipDir. If the function returns SkipDir when invoked on a directory,
// Walk skips the directory's contents entirely. If the function returns SkipDir
// when invoked on a non-directory file, Walk skips the remaining files in the
// containing directory.
//
// Especially, for the FileInfo is nil when WalkFunc is called on the root path
// of the repository.
type WalkFunc func(path string, info *FileInfo, err error) error

// Walk walks the github repository tree, calling walkFn for each file or
// directory in the tree, including the path specified. All errors that arise
// visiting files and directories are filtered by walkFn. The files are walked in
// lexical order, which makes the output deterministic but means that for very
// large directories Walk can be inefficient.
// Walk does not follow symbolic links.
func Walk(ctx context.Context, owner, repo, path string, opt *WalkOptions, walkFn WalkFunc) error {
	var tc *http.Client

	// construct the github client
	if opt != nil && opt.Token != "" {
		ts := oauth2.StaticTokenSource(
			&oauth2.Token{AccessToken: opt.Token},
		)
		tc = oauth2.NewClient(ctx, ts)
	}

	client := github.NewClient(tc)

	var getOpt *github.RepositoryContentGetOptions
	if opt != nil && opt.Ref != "" {
		getOpt = &github.RepositoryContentGetOptions{Ref: opt.Ref}
	}

	info, err := stat(ctx, owner, repo, path, client, getOpt)
	if err != nil {
		err = walkFn(path, nil, err)
	} else {
		err = walk(ctx, owner, repo, path, client, getOpt, info, walkFn)
	}

	if err == SkipDir {
		return nil
	}
	return err
}

func walk(ctx context.Context, owner, repo, path string, client *github.Client, opt *github.RepositoryContentGetOptions, info *FileInfo, walkFn WalkFunc) error {
	// If walk is called against the repo root, the info is nil
	if info != nil && !info.IsDir() {
		return walkFn(path, info, nil)
	}

	names, err := readDirNames(ctx, owner, repo, path, client, opt)
	err1 := walkFn(path, info, err)
	// If err != nil, walk can't walk into this directory.
	// err1 != nil means walkFn want walk to skip this directory or stop walking.
	// Therefore, if one of err and err1 isn't nil, walk will return.
	if err != nil || err1 != nil {
		// The caller's behavior is controlled by the return value, which is decided
		// by walkFn. walkFn may ignore err and return nil.
		// If walkFn returns SkipDir, it will be handled by the caller.
		// So walk should return whatever walkFn returns.
		return err1
	}

	for _, name := range names {
		filename := filepath.Join(path, name)
		fileInfo, err := stat(ctx, owner, repo, filename, client, opt)
		if err != nil {
			if err := walkFn(filename, fileInfo, err); err != nil && err != SkipDir {
				return err
			}
		} else {
			err = walk(ctx, owner, repo, filename, client, opt, fileInfo, walkFn)
			if err != nil {
				if !fileInfo.IsDir() || err != SkipDir {
					return err
				}
			}
		}
	}
	return nil
}

func newFileInfo(c github.RepositoryContent) *FileInfo {
	return &FileInfo{
		raw:         c,
		Type:        *c.Type,
		Target:      c.Target,
		Encoding:    c.Encoding,
		Size:        *c.Size,
		Name:        *c.Name,
		Path:        *c.Path,
		Content:     c.Content,
		SHA:         *c.SHA,
		URL:         *c.URL,
		GitURL:      *c.GitURL,
		HTMLURL:     *c.HTMLURL,
		DownloadURL: c.DownloadURL,
	}
}

func stat(ctx context.Context, owner, repo, path string, client *github.Client, opt *github.RepositoryContentGetOptions) (*FileInfo, error) {
	// The root directory of the repo has no meta info
	if path == "" {
		return nil, nil
	}

	parentPath := filepath.Dir(path)
	// If the `path` is at the root level, then we explicitly turn its parent path to be empty
	// string, which indicates to get repository content at the root level.
	if parentPath == "." {
		parentPath = ""
	}

	_, dircontent, _, err := client.Repositories.GetContents(ctx, owner, repo, parentPath, opt)
	if err != nil {
		return nil, err
	}

	for _, content := range dircontent {
		if content == nil {
			continue
		}
		if *content.Name == filepath.Base(path) {
			return newFileInfo(*content), nil
		}
	}

	return nil, fmt.Errorf("no such path found: %s", path)
}

func readDirNames(ctx context.Context, owner, repo, path string, client *github.Client, opt *github.RepositoryContentGetOptions) ([]string, error) {
	_, dircontent, _, err := client.Repositories.GetContents(ctx, owner, repo, path, opt)
	if err != nil {
		return nil, err
	}
	entries := []string{}
	for _, content := range dircontent {
		entries = append(entries, *content.Name)
	}
	sort.Strings(entries)
	return entries, nil
}

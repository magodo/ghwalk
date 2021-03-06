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

	// FileInfo of file (rather than dir) will contain file only FileInfo's
	EnableFileOnlyInfo bool

	// Reverse search ordering
	Reverse bool
}

type FileType string

const (
	FileTypeFile    FileType = "file"
	FileTypeDir     FileType = "dir"
	FileTypeSymlink FileType = "symlink"
)

type FileInfo struct {
	raw github.RepositoryContent

	Type    FileType
	Size    int
	Name    string
	Path    string
	SHA     string
	URL     string
	GitURL  string
	HTMLURL string

	FileOnlyInfo *FileOnlyInfo
}

type FileOnlyInfo struct {
	// Target is only set if the type is "symlink" and the target is not a normal file.
	Target *string
	// Encoding is only set if the type is "file" (but not "symlink")
	Encoding *string
	// Content contains the actual file content, which may be encoded.
	// Callers should call GetContent which will decode the content if
	// necessary.
	//
	// Content is only set if the type is "file" (but not "symlink")
	Content     *string
	DownloadURL string
}

func (f *FileInfo) IsDir() bool {
	return f.Type == FileTypeDir
}

func (f *FileInfo) GetContent() (string, error) {
	return f.raw.GetContent()
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

// PathFilterFunc allows users to filter a file/directory before sending any Github API to retrieve its metadata, if it returns true.
// This is useful when you know some pattern of the target path to walk, this can speed up the process by early skip those unrelated
// files/directories without sending any API.
type PathFilterFunc func(path string, info *FileInfo) bool

// Walk walks the github repository tree, calling walkFn for each file or
// directory in the tree, including the path specified. All errors that arise
// visiting files and directories are filtered by walkFn. The files are walked in
// lexical order, which makes the output deterministic but means that for very
// large directories Walk can be inefficient.
// Walk does not follow symbolic links.
func Walk(ctx context.Context, owner, repo, path string, opt *WalkOptions, walkFn WalkFunc, filterFn PathFilterFunc) error {

	var tc *http.Client

	// construct the github client
	if opt != nil && opt.Token != "" {
		ts := oauth2.StaticTokenSource(
			&oauth2.Token{AccessToken: opt.Token},
		)
		tc = oauth2.NewClient(ctx, ts)
	}

	client := github.NewClient(tc)

	info, err := stat(ctx, owner, repo, path, client, opt)
	if err != nil {
		err = walkFn(path, nil, err)
	} else {
		if filterFn != nil && filterFn(path, info) {
			return nil
		}
		err = walk(ctx, owner, repo, path, client, opt, info, walkFn, filterFn)
	}

	if err == SkipDir {
		return nil
	}
	return err
}

func walk(ctx context.Context, owner, repo, path string, client *github.Client, opt *WalkOptions, info *FileInfo, walkFn WalkFunc, filterFn PathFilterFunc) error {
	// If walk is called against the repo root, the info is nil
	if info != nil && !info.IsDir() {
		return walkFn(path, info, nil)
	}

	entries, err := readDirEntries(ctx, owner, repo, path, client, opt)
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

	for _, entry := range entries {
		filename := filepath.Join(path, entry.Name)

		if filterFn != nil && filterFn(filename, &entry) {
			continue
		}

		fileInfo, err := stat(ctx, owner, repo, filename, client, opt)
		if err != nil {
			if err := walkFn(filename, fileInfo, err); err != nil && err != SkipDir {
				return err
			}
		} else {
			err = walk(ctx, owner, repo, filename, client, opt, fileInfo, walkFn, filterFn)
			if err != nil {
				if !fileInfo.IsDir() || err != SkipDir {
					return err
				}
			}
		}
	}
	return nil
}

func newFileInfo(c github.RepositoryContent, includeDetail bool) *FileInfo {
	fileinfo := &FileInfo{
		raw:     c,
		Type:    FileType(*c.Type),
		Size:    *c.Size,
		Name:    *c.Name,
		Path:    *c.Path,
		SHA:     *c.SHA,
		URL:     *c.URL,
		GitURL:  *c.GitURL,
		HTMLURL: *c.HTMLURL,
	}

	if includeDetail {
		fileinfo.FileOnlyInfo = &FileOnlyInfo{
			Encoding:    c.Encoding,
			Content:     c.Content,
			Target:      c.Target,
			DownloadURL: *c.DownloadURL,
		}
	}

	return fileinfo
}

func stat(ctx context.Context, owner, repo, path string, client *github.Client, opt *WalkOptions) (*FileInfo, error) {
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

	_, dircontent, _, err := client.Repositories.GetContents(ctx, owner, repo, parentPath, newRepositoryGetContentOptions(opt))
	if err != nil {
		return nil, err
	}

	for _, content := range dircontent {
		if content == nil {
			continue
		}
		if *content.Name == filepath.Base(path) {
			fileInfo := newFileInfo(*content, false)

			// users specify to enable file only info, then we need to invoke another API call against the path to the file
			if !fileInfo.IsDir() && opt != nil && opt.EnableFileOnlyInfo {
				filecontent, _, _, err := client.Repositories.GetContents(ctx, owner, repo, path, newRepositoryGetContentOptions(opt))
				if err != nil {
					return nil, err
				}
				return newFileInfo(*filecontent, true), nil
			}
			return fileInfo, nil
		}
	}

	return nil, fmt.Errorf("no such path found: %s", path)
}

func readDirEntries(ctx context.Context, owner, repo, path string, client *github.Client, opt *WalkOptions) ([]FileInfo, error) {
	_, dircontent, _, err := client.Repositories.GetContents(ctx, owner, repo, path, newRepositoryGetContentOptions(opt))
	if err != nil {
		return nil, err
	}
	entryNames := make([]string, 0, len(dircontent))
	entryMap := map[string]FileInfo{}
	for _, content := range dircontent {
		entryMap[*content.Name] = *newFileInfo(*content, false)
		entryNames = append(entryNames, *content.Name)
	}

	if opt != nil && opt.Reverse {
		sort.Sort(sort.Reverse(sort.StringSlice(entryNames)))
	} else {
		sort.Strings(entryNames)
	}

	entries := make([]FileInfo, 0, len(entryMap))
	for _, name := range entryNames {
		entries = append(entries, entryMap[name])
	}
	return entries, nil
}

func newRepositoryGetContentOptions(opt *WalkOptions) *github.RepositoryContentGetOptions {
	if opt == nil {
		return nil
	}
	return &github.RepositoryContentGetOptions{
		Ref: opt.Ref,
	}
}

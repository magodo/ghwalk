ghwalk is a Go library to walk a Github repository path similar to `filepath.Walk()`.

## Example

### Basic 

```go
package main

import (
	"context"
	"fmt"

	"github.com/magodo/ghwalk"
)

func main() {
	ghwalk.Walk(context.TODO(), "magodo", "ghwalk", "testdata", nil,
		func(path string, info *ghwalk.FileInfo, err error) error {
			if err != nil {
				return err
			}

			// repo root will be called with nil info
			if info == nil {
				return nil
			}

			fmt.Printf("%s\n", path)
			return nil
		})
}
```

Output:

```bash
testdata
testdata/a
testdata/b
testdata/dir
testdata/dir/c
testdata/link_dir
```

### Enable File Only Info

This will introduce extra API invocations on **file** blob, so the example below asks for a Github access token to avoid hitting ratelimit:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/magodo/ghwalk"
)

var sep = strings.Repeat("=", 20)

func main() {
	token := os.Args[1]
	if err := ghwalk.Walk(context.TODO(), "magodo", "ghwalk", "testdata",
		&ghwalk.WalkOptions{
			Token:              token,
			EnableFileOnlyInfo: true,
		},
		func(path string, info *ghwalk.FileInfo, err error) error {
			if err != nil {
				return err
			}

			// repo root will be called with nil info
			if info == nil {
				return nil
			}

			switch info.Type {
			case ghwalk.FileTypeDir:
				fmt.Printf("%s\n%s (%s)\n%s\n", sep, info.Path, info.Type, sep)
			case ghwalk.FileTypeFile:
				content, _ := info.GetContent()
				if len(content) > 50 {
					content = content[:50] + "\n..."
				}
				fmt.Printf("%s\n%s (%s)\n%s\n%s\n", sep, info.Path, info.Type, sep, content)
			case ghwalk.FileTypeSymlink:
				fmt.Printf("%s\n%s -> %s (%s)\n%s\n", sep, info.Path, *info.FileOnlyInfo.Target, info.Type, sep)
			}
			return nil
		}); err != nil {
		log.Fatal(err)
	}
}
```

Output:

```bash
====================
testdata (dir)
====================
====================
testdata/a (file)
====================
content of a

====================
testdata/b (file)
====================
content of b

====================
testdata/dir (dir)
====================
====================
testdata/dir/c (file)
====================
content of c in dir

====================
testdata/link_dir -> dir (symlink)
====================
```

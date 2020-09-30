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

// main executable.
package main

import (
	"os"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx/internal/core"
)

func main() {
	s, ok := core.New(os.Args[1:])
	if !ok {
		os.Exit(1)
	}
	s.Wait()
}

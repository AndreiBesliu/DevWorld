package defs

import "github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx/internal/logger"

// Reader is an entity that can read a stream.
type Reader interface {
	logger.Writer
	Close()
	APIReaderDescribe() *APIPathReader
}

package core

import (
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx/internal/defs"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx/internal/logger"
)

// sourceRedirect is a source that redirects to another one.
type sourceRedirect struct{}

func (*sourceRedirect) Log(logger.Level, string, ...any) {
}

// APISourceDescribe implements source.
func (*sourceRedirect) APISourceDescribe() *defs.APIPathSource {
	return &defs.APIPathSource{
		Type: defs.APIPathSourceTypeRedirect,
		ID:   "",
	}
}

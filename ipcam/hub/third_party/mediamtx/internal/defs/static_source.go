package defs

import (
	"context"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx/internal/conf"
)

// StaticSourceRunParams is the set of params passed to Run().
type StaticSourceRunParams struct {
	Context        context.Context
	ResolvedSource string
	Conf           *conf.Path
	ReloadConf     chan *conf.Path
}

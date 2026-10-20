package httpp_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx/internal/protocols/httpp"
)

func TestParseContentType(t *testing.T) {
	v := httpp.ParseContentType("text/plain; charset=utf-8")
	require.Equal(t, "text/plain", v)

	v = httpp.ParseContentType("text/plain")
	require.Equal(t, "text/plain", v)
}

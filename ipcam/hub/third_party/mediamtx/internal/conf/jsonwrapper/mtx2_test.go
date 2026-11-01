package jsonwrapper_test

// The decoder test of patch MTX-2 (hub/third_party/PATCHES.md): a type that
// implements jsonwrapper.NullRejecter refuses JSON null even behind a
// pointer, where null otherwise leaves the pointer nil, so that an explicit
// null cannot pass for an absent key.

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx/internal/conf/jsonwrapper"
)

type mtx2NonNullable []string

func (mtx2NonNullable) RejectsJSONNull() {}

var _ jsonwrapper.NullRejecter = mtx2NonNullable(nil)

func TestMTX2_NullRejecterBehindPointer(t *testing.T) {
	type Inner struct {
		Items *mtx2NonNullable `json:"items"`
	}
	type Data struct {
		Items *mtx2NonNullable `json:"items"`
		Other *[]string        `json:"other"`
		Inner Inner            `json:"inner"`
	}

	t.Run("null is refused", func(t *testing.T) {
		var data Data
		err := jsonwrapper.Unmarshal([]byte(`{"items": null}`), &data)
		require.EqualError(t, err, `cannot set "items" to null`)

		data = Data{Items: &mtx2NonNullable{"a"}}
		err = jsonwrapper.Unmarshal([]byte(`{"items": null}`), &data)
		require.EqualError(t, err, `cannot set "items" to null`)
		require.Equal(t, &mtx2NonNullable{"a"}, data.Items)

		err = jsonwrapper.Unmarshal([]byte(`{"inner": {"items": null}}`), &data)
		require.EqualError(t, err, `cannot set "inner.items" to null`)
	})

	t.Run("a list decodes", func(t *testing.T) {
		var data Data
		err := jsonwrapper.Unmarshal([]byte(`{"items": ["a", "b"]}`), &data)
		require.NoError(t, err)
		require.Equal(t, &mtx2NonNullable{"a", "b"}, data.Items)

		err = jsonwrapper.Unmarshal([]byte(`{"items": []}`), &data)
		require.NoError(t, err)
		require.Equal(t, &mtx2NonNullable{}, data.Items)
	})

	t.Run("absent leaves the pointer nil", func(t *testing.T) {
		var data Data
		err := jsonwrapper.Unmarshal([]byte(`{}`), &data)
		require.NoError(t, err)
		require.Nil(t, data.Items)
	})

	t.Run("other pointers keep upstream's rule", func(t *testing.T) {
		data := Data{Other: &[]string{"a"}}
		err := jsonwrapper.Unmarshal([]byte(`{"other": null}`), &data)
		require.NoError(t, err)
		require.Nil(t, data.Other)
	})
}

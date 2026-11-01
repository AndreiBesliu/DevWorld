package gdrive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"path"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
)

// PutSmall writes data to key in one request, or replaces the bytes of the
// file that an earlier PutSmall wrote there: the store keeps a small file's
// ID by its path, as a folder's, so a README or a catalog stays one file,
// found with no files.list. Its first write gets an ID from
// files.generateIds, stores it and creates the file with it (a multipart
// files.create); a later one updates the file's bytes (files.update), which
// Drive keeps as the file's new revision, a reader seeing the old bytes or
// the new. Either takes a create slot first.
func (d *Drive) PutSmall(ctx context.Context, key storage.ObjectKey, data []byte) (storage.ObjectRef, error) {
	if err := checkKey(key); err != nil {
		return storage.ObjectRef{}, err
	}
	var ref storage.ObjectRef
	err := d.into(ctx, key, nil, func(parent string) error {
		var err error
		ref, err = d.writeSmall(ctx, key, data, parent)
		return err
	})
	return ref, err
}

// writeSmall is PutSmall into the folder parent.
func (d *Drive) writeSmall(ctx context.Context, key storage.ObjectKey, data []byte, parent string) (storage.ObjectRef, error) {
	if err := d.slot(methodCreate); err != nil {
		return storage.ObjectRef{}, err
	}
	p := path.Join(d.rootName, string(key))
	id, ok, err := d.stored(ctx, p)
	switch {
	case err != nil:
		return storage.ObjectRef{}, err
	case ok:
		ref, err := d.updateSmall(ctx, id, key, data)
		if !errors.Is(err, storage.ErrNotFound) {
			return ref, err
		}
		// The file is gone: it is created again with its ID.
	default:
		if id, err = d.storeNewID(ctx, p, nil); err != nil {
			return storage.ObjectRef{}, err
		}
	}
	ref, created, err := d.createSmall(ctx, id, key, data, parent)
	if err != nil || created {
		return ref, err
	}
	return d.updateSmall(ctx, id, key, data) // another process created it first
}

// createSmall creates the small file id named key's base in parent with
// data, and reports whether it did: a 409 says that the file exists.
func (d *Drive) createSmall(ctx context.Context, id string, key storage.ObjectKey, data []byte, parent string) (storage.ObjectRef, bool, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	meta, err := mw.CreatePart(textproto.MIMEHeader{"Content-Type": {jsonType}})
	if err == nil {
		err = json.NewEncoder(meta).Encode(map[string]any{"id": id, "name": path.Base(string(key)), "parents": []string{parent}})
	}
	var content io.Writer
	if err == nil {
		content, err = mw.CreatePart(textproto.MIMEHeader{"Content-Type": {contentType(key)}})
	}
	if err == nil {
		_, err = content.Write(data)
	}
	if err == nil {
		err = mw.Close()
	}
	if err != nil {
		return storage.ObjectRef{}, false, fmt.Errorf("gdrive: %s: %w: %w", methodCreate, storage.ErrPermanent, err)
	}
	a, err := d.send(ctx, call{
		method: methodCreate, verb: http.MethodPost, url: d.uploadBase + "/upload/drive/v3/files?uploadType=multipart&fields=id,md5Checksum,size",
		header: map[string]string{"Content-Type": "multipart/related; boundary=" + mw.Boundary()},
		body:   bytes.NewReader(body.Bytes()), length: int64(body.Len()),
	}, http.StatusOK, http.StatusConflict)
	if err != nil {
		return storage.ObjectRef{}, false, err
	}
	if a.StatusCode == http.StatusConflict {
		a.Close()
		return storage.ObjectRef{}, false, nil
	}
	var f driveFile
	if err := a.decode(methodCreate, &f); err != nil {
		return storage.ObjectRef{}, false, err
	}
	return f.ref(key), true, nil
}

// updateSmall replaces the bytes of the small file id with data.
func (d *Drive) updateSmall(ctx context.Context, id string, key storage.ObjectKey, data []byte) (storage.ObjectRef, error) {
	a, err := d.send(ctx, call{
		method: methodUpdate, verb: http.MethodPatch,
		url:    d.uploadBase + "/upload/drive/v3/files/" + url.PathEscape(id) + "?uploadType=media&fields=id,md5Checksum,size",
		header: map[string]string{"Content-Type": contentType(key)}, body: bytes.NewReader(data), length: int64(len(data)),
	}, http.StatusOK)
	if err != nil {
		return storage.ObjectRef{}, err
	}
	var f driveFile
	if err := a.decode(methodUpdate, &f); err != nil {
		return storage.ObjectRef{}, err
	}
	return f.ref(key), nil
}

// contentType is the MIME type of a small file, by its key's extension.
func contentType(key storage.ObjectKey) string {
	if t := mime.TypeByExtension(path.Ext(string(key))); t != "" {
		return t
	}
	return "application/octet-stream"
}

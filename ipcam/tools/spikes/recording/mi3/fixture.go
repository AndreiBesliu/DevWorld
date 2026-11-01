// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi3

import (
	"bufio"
	cryptorand "crypto/rand"
	"errors"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"

	"filippo.io/age"
)

// fixtureBytes is 5 min of a 4 Mbps main stream.
const fixtureBytes = 300 * cameraBps / 8

func (o Options) fixtureBytes() int64 {
	if o.FixtureBytes > 0 {
		return o.FixtureBytes
	}
	return fixtureBytes
}

// fixture is the 5-min segment of the playback, the export and the upload
// (G20), in random bytes: the cost of age does not depend on the content.
// plain is the clear segment, segment its age encryption to identity's key.
type fixture struct {
	dir, plain, segment, identity string
}

// makeFixture writes the fixture of size bytes in dir.
func makeFixture(dir string, size int64) (fixture, error) {
	fx := fixture{dir: dir, plain: filepath.Join(dir, "segment.mp4"), segment: filepath.Join(dir, "segment.mp4.age"), identity: filepath.Join(dir, "fixture.key")}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fx, err
	}
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return fx, err
	}
	if err := os.WriteFile(fx.identity, []byte(id.String()+"\n"), 0o600); err != nil {
		return fx, err
	}
	var seed [32]byte
	if _, err := cryptorand.Read(seed[:]); err != nil {
		return fx, err
	}
	if err := writeFile(fx.plain, func(w io.Writer) error {
		_, err := io.CopyN(w, rand.NewChaCha8(seed), size)
		return err
	}); err != nil {
		return fx, err
	}
	return fx, writeFile(fx.segment, func(w io.Writer) error {
		in, err := os.Open(fx.plain)
		if err != nil {
			return err
		}
		defer func() { _ = in.Close() }()
		enc, err := age.Encrypt(w, id.Recipient())
		if err != nil {
			return err
		}
		if _, err := io.Copy(enc, in); err != nil {
			return err
		}
		return enc.Close()
	})
}

// writeFile writes path, 0600, with write.
func writeFile(path string, write func(io.Writer) error) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600) //nolint:gosec // the run's own folder
	if err != nil {
		return err
	}
	bw := bufio.NewWriterSize(f, 1<<20)
	err = write(bw)
	if err == nil {
		err = bw.Flush()
	}
	return errors.Join(err, f.Close())
}

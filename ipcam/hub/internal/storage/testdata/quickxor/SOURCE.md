# QuickXorHash test vectors

`vectors.json` holds the `testVectors` table of rclone's QuickXorHash tests,
an implementation independent of this package's (P34): rclone's is a Go port
of namazso/QuickXorHash (BSD Zero Clause), and `Digest` follows the
algorithm as Microsoft's reference describes it.

- Repository: https://github.com/rclone/rclone
- File: `backend/onedrive/quickxorhash/quickxorhash_test.go`, the `testVectors` table
- Version: tag `v1.75.2`, commit `be8d6044eacb843b74d335db0c5fbe09222031fe`
- Fetched as the module zip `github.com/rclone/rclone@v1.75.2` from
  proxy.golang.org (2026-10-30); the commit is the zip's `Origin.Hash`. The
  test file's SHA-256 is
  `0e5e9620fd28fb51f3c571c99c947702774b3982dbd3f5c74da944cfde80f858`.
- License: MIT, rclone's `COPYING` (below).

The table's 70 rows became the 70 entries of `vectors.json`, in the same
order: `size` is the row's size, `data` its input in standard base64 with the
Go literal's line breaks removed, and `quickxor` its expected hash in standard
base64. Each `data` decodes to `size` bytes, and each `quickxor` to 20 bytes.

## rclone's license

```
Copyright (C) 2012 by Nick Craig-Wood http://www.craig-wood.com/nick/

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
```

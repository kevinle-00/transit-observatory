package storage

import (
	"bytes"
	"io"
	"os"
)

type byteSource struct {
	data []byte
}

func BytesSource(data []byte) Source {
	return byteSource{data: bytes.Clone(data)}
}

func (source byteSource) Open() (io.ReadSeekCloser, error) {
	return readSeekNopCloser{Reader: bytes.NewReader(source.data)}, nil
}

func (source byteSource) Size() int64 {
	return int64(len(source.data))
}

type fileSource struct {
	path string
	size int64
}

func FileSource(path string) (Source, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, os.ErrInvalid
	}
	return fileSource{path: path, size: info.Size()}, nil
}

func (source fileSource) Open() (io.ReadSeekCloser, error) {
	return os.Open(source.path)
}

func (source fileSource) Size() int64 {
	return source.size
}

type readSeekNopCloser struct {
	*bytes.Reader
}

func (readSeekNopCloser) Close() error { return nil }

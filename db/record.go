//go:generate protoc -I . -I ../../../../ -I ../../../../github.com/gogo/protobuf/protobuf --gogofast_out=. record.proto

package db

import (
	"bytes"
	"compress/gzip"
	"io"
)

func (r *Record) Reader() (io.Reader, error) {
	if !r.Compressed {
		return bytes.NewReader(r.MessageData), nil
	}

	br, err := gzip.NewReader(bytes.NewReader(r.MessageData))
	if err != nil {
		return nil, err
	}
	return br, nil
}

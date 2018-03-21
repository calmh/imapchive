//go:generate protoc -I . -I ../../../ -I ../../../github.com/gogo/protobuf/protobuf --gogofast_out=. record.proto

package db

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"io/ioutil"
	"log"
	"os"
	"sync"
)

type DB struct {
	mut     sync.Mutex
	name    string
	labels  map[uint32][]string
	offsets map[uint32]int64
	dirty   int
	fd      *os.File
	buf     []byte
}

func Open(name string) (*DB, error) {
	fd, err := os.OpenFile(name, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	db := &DB{
		name:    name,
		labels:  make(map[uint32][]string),
		offsets: make(map[uint32]int64),
		fd:      fd,
	}

	if err := db.readIndex(); err != nil {
		if !os.IsNotExist(err) {
			log.Println("Reading index:", err, "(reindexing)")
		}
		db.labels = make(map[uint32][]string)
		db.offsets = make(map[uint32]int64)
		db.fd.Seek(0, io.SeekStart)
	}

	if err := db.scan(); err != nil {
		fd.Close()
		return nil, err
	}

	if db.dirty > 0 {
		db.writeIndex()
	}

	db.fd.Seek(0, io.SeekStart)
	return db, err
}

func (db *DB) scan() error {
	for {
		offs, _ := db.fd.Seek(0, io.SeekCurrent)
		rec, err := db.ReadRecord()
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}

		if rec.Deleted {
			db.offsets[rec.MessageID] = -1
			delete(db.labels, rec.MessageID)
			continue
		}

		db.offsets[rec.MessageID] = offs
		db.labels[rec.MessageID] = rec.Labels
		db.dirty++
	}
	return nil
}

func (db *DB) writeIndex() error {
	fd, err := os.Create(db.name + ".idx.tmp")
	if err != nil {
		return err
	}

	offs, _ := db.fd.Seek(0, io.SeekEnd)
	idx := Index{
		FileOffset: offs,
	}
	for msg, offs := range db.offsets {
		idx.Records = append(idx.Records, &IndexRecord{
			MessageID:  msg,
			FileOffset: offs,
			Labels:     db.labels[msg],
		})
	}

	bs, _ := idx.Marshal()
	hash := sha256.Sum256(bs)
	if _, err := fd.Write(hash[:]); err != nil {
		fd.Close()
		return err
	}

	bs = compress(bs)
	if _, err := fd.Write(bs); err != nil {
		fd.Close()
		return err
	}
	if err := fd.Close(); err != nil {
		return err
	}

	if err := os.Rename(db.name+".idx.tmp", db.name+".idx"); err != nil {
		return err
	}

	db.dirty = 0
	return nil
}

func (db *DB) readIndex() error {
	bs, err := ioutil.ReadFile(db.name + ".idx")
	if err != nil {
		return err
	}

	dec, err := decompress(bs[32:])
	if err != nil {
		return err
	}

	hash := sha256.Sum256(dec)
	if !bytes.Equal(hash[:], bs[:32]) {
		return errors.New("index corrupt")
	}

	var idx Index
	if err := idx.Unmarshal(dec); err != nil {
		return err
	}

	for _, rec := range idx.Records {
		db.labels[rec.MessageID] = rec.Labels
		db.offsets[rec.MessageID] = rec.FileOffset
	}

	if _, err := db.fd.Seek(idx.FileOffset, io.SeekStart); err != nil {
		return err
	}
	return nil
}

func (db *DB) Rewind() error {
	_, err := db.fd.Seek(0, io.SeekStart)
	return err
}

func (db *DB) ReadRecord() (MessageRecord, error) {
	db.mut.Lock()
	defer db.mut.Unlock()

	if db.buf == nil {
		db.buf = make([]byte, 65536)
	}
	if _, err := io.ReadFull(db.fd, db.buf[:4]); err != nil {
		return MessageRecord{}, err
	}

	size := int(binary.BigEndian.Uint32(db.buf))
	if len(db.buf) < size {
		db.buf = make([]byte, size)
	}
	if _, err := io.ReadFull(db.fd, db.buf[:size]); err != nil {
		return MessageRecord{}, err
	}

	bs, err := decompress(db.buf[:size])
	if err != nil {
		return MessageRecord{}, err
	}

	var rec MessageRecord
	if err := rec.Unmarshal(bs); err != nil {
		return MessageRecord{}, err
	}

	return rec, nil
}

func (db *DB) Size() int {
	defer db.mut.Unlock()
	db.mut.Lock()
	return len(db.offsets)
}

func (db *DB) Have(msgid uint32) bool {
	defer db.mut.Unlock()
	db.mut.Lock()
	return db.offsets[msgid] > 0
}

func (db *DB) Labels(msgid uint32) []string {
	defer db.mut.Unlock()
	db.mut.Lock()
	return db.labels[msgid]
}

func (db *DB) SetLabels(msgid uint32, labels []string) error {
	defer db.mut.Unlock()
	db.mut.Lock()

	db.labels[msgid] = labels

	rec := MessageRecord{
		MessageID: msgid,
		Labels:    labels,
	}

	return db.writeRecord(rec)
}

func (db *DB) WriteMessage(msgid uint32, data []byte, labels []string) error {
	defer db.mut.Unlock()
	db.mut.Lock()

	offs, _ := db.fd.Seek(0, io.SeekEnd)
	db.offsets[msgid] = offs
	db.labels[msgid] = labels

	hash := sha256.Sum256(data)

	rec := MessageRecord{
		MessageID:   msgid,
		MessageData: data,
		MessageHash: hash[:],
		Labels:      labels,
	}

	return db.writeRecord(rec)
}

func (db *DB) DeleteMessage(msgid uint32) error {
	defer db.mut.Unlock()
	db.mut.Lock()

	db.offsets[msgid] = -1
	delete(db.labels, msgid)

	rec := MessageRecord{
		MessageID: msgid,
		Deleted:   true,
	}

	return db.writeRecord(rec)
}

func (db *DB) writeRecord(rec MessageRecord) error {
	bs, err := rec.Marshal()
	if err != nil {
		return err
	}

	bs = compress(bs)

	size := make([]byte, 4)
	binary.BigEndian.PutUint32(size, uint32(len(bs)))
	if _, err := db.fd.Write(size); err != nil {
		return err
	}
	if _, err := db.fd.Write(bs); err != nil {
		return err
	}

	db.dirty++

	if db.dirty >= 1000 {
		db.writeIndex()
	}

	return nil
}

func (db *DB) WriteClose() error {
	defer db.mut.Unlock()
	db.mut.Lock()
	return nil
}

func compress(data []byte) []byte {
	buf := new(bytes.Buffer)
	gw := gzip.NewWriter(buf)
	gw.Write(data)
	gw.Close()
	return buf.Bytes()
}

func decompress(data []byte) ([]byte, error) {
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return ioutil.ReadAll(gr)
}

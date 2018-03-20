package db

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/binary"
	"io"
	"os"
	"sync"
)

type DB struct {
	mut       sync.Mutex
	labels    map[uint32][]string
	haveMsgID map[uint32]bool
	fd        *os.File
	buf       []byte
}

func OpenWrite(name string) (*DB, error) {
	fd, err := os.OpenFile(name, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	db := &DB{
		labels:    make(map[uint32][]string),
		haveMsgID: make(map[uint32]bool),
		fd:        fd,
	}

	if err := db.scan(); err != nil {
		fd.Close()
		return nil, err
	}

	return db, nil
}

func OpenRead(name string) (*DB, error) {
	fd, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	db := &DB{
		labels:    make(map[uint32][]string),
		haveMsgID: make(map[uint32]bool),
		fd:        fd,
	}

	if err := db.scan(); err != nil {
		fd.Close()
		return nil, err
	}

	db.fd.Seek(0, 0)

	return db, nil
}

func (db *DB) scan() error {
	for {
		rec, err := db.ReadRecord()
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}

		if rec.Deleted {
			delete(db.haveMsgID, rec.MessageID)
			delete(db.labels, rec.MessageID)
			continue
		}

		db.haveMsgID[rec.MessageID] = true
		db.labels[rec.MessageID] = rec.Labels
	}
	return nil
}

func (db *DB) ReadRecord() (Record, error) {
	db.mut.Lock()
	defer db.mut.Unlock()

	if db.buf == nil {
		db.buf = make([]byte, 65536)
	}
	if _, err := io.ReadFull(db.fd, db.buf[:4]); err != nil {
		return Record{}, err
	}

	size := int(binary.BigEndian.Uint32(db.buf))
	if len(db.buf) < size {
		db.buf = make([]byte, size)
	}
	if _, err := io.ReadFull(db.fd, db.buf[:size]); err != nil {
		return Record{}, err
	}

	var rec Record
	if err := rec.Unmarshal(db.buf[:size]); err != nil {
		return Record{}, err
	}

	return rec, nil
}

func (db *DB) Size() int {
	defer db.mut.Unlock()
	db.mut.Lock()
	return len(db.haveMsgID)
}

func (db *DB) Have(msgid uint32) bool {
	defer db.mut.Unlock()
	db.mut.Lock()
	return db.haveMsgID[msgid]
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

	rec := Record{
		MessageID: msgid,
		Labels:    labels,
	}

	return db.writeRecord(rec)
}

func (db *DB) WriteMessage(msgid uint32, data []byte, labels []string) error {
	defer db.mut.Unlock()
	db.mut.Lock()

	db.haveMsgID[msgid] = true
	db.labels[msgid] = labels

	buf := new(bytes.Buffer)
	gz := gzip.NewWriter(buf)
	gz.Write(data)
	gz.Close()

	hash := sha256.Sum256(data)

	rec := Record{
		MessageID:   msgid,
		MessageData: buf.Bytes(),
		MessageHash: hash[:],
		Compressed:  true,
		Labels:      labels,
	}

	return db.writeRecord(rec)
}

func (db *DB) DeleteMessage(msgid uint32) error {
	defer db.mut.Unlock()
	db.mut.Lock()

	delete(db.haveMsgID, msgid)
	delete(db.labels, msgid)

	rec := Record{
		MessageID: msgid,
		Deleted:   true,
	}

	return db.writeRecord(rec)
}

func (db *DB) writeRecord(rec Record) error {
	bs, err := rec.Marshal()
	if err != nil {
		return err
	}

	size := make([]byte, 4)
	binary.BigEndian.PutUint32(size, uint32(len(bs)))
	if _, err := db.fd.Write(size); err != nil {
		return err
	}
	if _, err := db.fd.Write(bs); err != nil {
		return err
	}

	return nil
}

func (db *DB) WriteClose() error {
	defer db.mut.Unlock()
	db.mut.Lock()
	return nil
}

package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alecthomas/kingpin"
	"kastelo.io/imapchive/db"
)

const (
	extension = ".imapchive"
)

var progress struct {
	toScan  int64
	scanned int64
	fetched int64
	labels  int64
}

func main() {
	flagEmail := kingpin.Flag("email", "Email address").String()
	flagPassword := kingpin.Flag("password", "Password").String()

	cmdFetch := kingpin.Command("fetch", "Fetch new mail")
	flagMailbox := cmdFetch.Arg("mailbox", "Mailbox name").Required().String()
	flagConcurrency := cmdFetch.Flag("concurrency", "Number of parallel fetch threads").Default("4").Int()

	cmdMbox := kingpin.Command("mbox", "Write an MBOX file with all messages to stdout")
	argFile := cmdMbox.Arg("file", "Archive file").Required().String()

	cmdList := kingpin.Command("list", "List available mailboxes")

	switch kingpin.Parse() {
	case cmdList.FullCommand():
		cl, err := Client(*flagEmail, *flagPassword, "")
		if err != nil {
			fmt.Println("Listing mailboxes:", err)
			os.Exit(1)
		}
		mailboxes, err := cl.Mailboxes()
		if err != nil {
			fmt.Println("Listing mailboxes:", err)
			os.Exit(1)
		}
		for _, mb := range mailboxes {
			fmt.Println(mb)
		}

	case cmdFetch.FullCommand():
		log.Println("Opening archive")
		dbName := strings.Replace(*flagMailbox, "/", "_", -1) + extension
		db, err := db.OpenWrite(dbName)
		if err != nil {
			log.Fatal(err)
		}

		log.Printf("Have %d messages", db.Size())
		uids := findNewUIDs(*flagEmail, *flagPassword, *flagMailbox, db)

		var wg sync.WaitGroup
		for i := 1; i <= *flagConcurrency; i++ {
			wg.Add(1)
			go func(i int) {
				fetchAndStore(*flagEmail, *flagPassword, *flagMailbox, i, db, uids)
				wg.Done()
			}(i)
		}

		go func() {
			for {
				time.Sleep(10 * time.Second)
				log.Printf("%d of %d scanned, %d fetched, %d labelupdated",
					atomic.LoadInt64(&progress.scanned), atomic.LoadInt64(&progress.toScan),
					atomic.LoadInt64(&progress.fetched), atomic.LoadInt64(&progress.labels))
			}
		}()

		wg.Wait()

		err = db.WriteClose()
		if err != nil {
			log.Fatal(err)
		}

	case cmdMbox.FullCommand():
		db, err := db.OpenRead(*argFile)
		if err != nil {
			fmt.Println("Opening archive:", err)
			os.Exit(1)
		}

		mbox(db, os.Stdout)
	}
}

func findNewUIDs(email, password, mailbox string, db *db.DB) chan msg {
	client, err := Client(email, password, mailbox)
	if err != nil {
		log.Fatal(err)
	}

	atomic.StoreInt64(&progress.toScan, int64(client.Mailbox.Messages))

	step := uint32(100)
	out := make(chan msg, step)

	go func() {
		begin := uint32(1)
		for begin < client.Mailbox.Messages {
			end := begin + step - 1

			msgs, err := client.MsgIDSearch(begin, end)
			if err != nil {
				log.Fatal(err)
			}
			atomic.AddInt64(&progress.scanned, int64(len(msgs)))

			begin += step

			fetch := 0
			for _, msg := range msgs {
				if !db.Have(msg.UID) {
					out <- msg
					fetch++
				} else if !sliceEquals(db.Labels(msg.UID), msg.Labels) {
					db.SetLabels(msg.UID, msg.Labels)
					atomic.AddInt64(&progress.labels, 1)
				}
			}

			if fetch == 0 && step < 3200 {
				// Scale up for faster scanning of known messages
				step *= 2
			} else if fetch > 0 && step > 100 {
				// Scale down to avoid timeouts and write reasonable label
				// chunks when we need to fetch lots of messages.
				step /= 2
			}
		}
		close(out)
	}()

	return out
}

func sliceEquals(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func fetchAndStore(email, password, mailbox string, id int, db *db.DB, msgids chan msg) {
	client, err := Client(email, password, mailbox)
	if err != nil {
		log.Fatal(err)
	}

loop:
	for {
		select {
		case msgid, ok := <-msgids:
			if !ok {
				break loop
			}

			body, err := client.GetMail(msgid.UID)
			if err != nil {
				log.Fatal(err)
			}

			err = db.WriteMessage(msgid.UID, body, msgid.Labels)
			if err != nil {
				log.Fatal(err)
			}

			atomic.AddInt64(&progress.fetched, 1)
		}
	}
}

func mbox(db *db.DB, wr io.Writer) {
	var nwritten int
	nl := []byte("\n")
	from := []byte("From ")
	esc := []byte(">")

	bwr := bufio.NewWriter(wr)

	for {
		rec, err := db.ReadRecord()
		if err == io.EOF {
			break
		}

		if !db.Have(rec.MessageID) {
			// Message has been deleted
			continue
		}

		rd, err := rec.Reader()
		if err != nil {
			continue
		}

		bwr.Write([]byte("From MAILER-DAEMON Thu Jan  1 01:00:00 1970\n"))
		if labels := db.Labels(rec.MessageID); len(labels) > 0 {
			fmt.Fprintf(bwr, "X-Gmail-Labels: %s\n", strings.Join(labels, ","))
		}
		sc := bufio.NewScanner(rd)
		for sc.Scan() {
			line := sc.Bytes()
			if bytes.HasPrefix(line, from) {
				bwr.Write(esc)
			}
			bwr.Write(line)
			bwr.Write(nl)
		}
		bwr.Write(nl)
		bwr.Flush()

		nwritten++
	}

	log.Printf("Wrote %d messages to stdout", nwritten)
}

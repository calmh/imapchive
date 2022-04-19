package main

import (
	"crypto/tls"
	"fmt"
	"sort"
	"time"

	"github.com/mxk/go-imap/imap"
)

type IMAPClient struct {
	imap.Client
}

func Client(server, email, password, mailbox string) (*IMAPClient, error) {
	tlsCfg := tls.Config{
		InsecureSkipVerify: true,
	}

	cl, err := imap.DialTLS(server, &tlsCfg)
	if err != nil {
		return nil, fmt.Errorf("connect to server: %w", err)
	}

	_, err = cl.Login(email, password)
	if err != nil {
		return nil, fmt.Errorf("login as %q: %w", email, err)
	}

	if mailbox != "" {
		_, err = cl.Select(mailbox, true)
		if err != nil {
			return nil, fmt.Errorf("select mailbox %q: %w", mailbox, err)
		}
	}

	go func() {
		// Discard unilateral server data now and then
		time.Sleep(1 * time.Second)
		cl.Data = nil
	}()

	return &IMAPClient{*cl}, nil
}

func (client *IMAPClient) GetMail(uid uint32) ([]byte, error) {
	var set = &imap.SeqSet{}
	set.AddNum(uid)

	cmd, err := client.UIDFetch(set, "RFC822")
	if err != nil {
		return nil, fmt.Errorf("get mail %d: %w", uid, err)
	}
	if len(cmd.Data) == 0 {
		return nil, fmt.Errorf("no data in mail %d", uid)
	}

	for cmd.InProgress() {
		err = client.Recv(-1)
		if err != nil {
			return nil, fmt.Errorf("get mail %d: %w", uid, err)
		}
	}

	resp := cmd.Data[0]
	body := imap.AsBytes(resp.MessageInfo().Attrs["RFC822"])

	return body, nil
}

func (client *IMAPClient) Mailboxes() ([]string, error) {
	cmd, err := imap.Wait(client.Client.List("", "*"))
	if err != nil {
		return nil, fmt.Errorf("mailbox list: %w", err)
	}

	var res []string
	for _, rsp := range cmd.Data {
		res = append(res, rsp.MailboxInfo().Name)
	}

	return res, nil
}

type msg struct {
	UID    uint32
	Labels []string
}

func (client *IMAPClient) MsgIDSearch(first, last uint32, withGmailLabels bool) ([]msg, error) {
	ss := fmt.Sprintf("%d:%d", first, last)
	seq, _ := imap.NewSeqSet(ss)
	labels := []string{"UID"}
	if withGmailLabels {
		labels = append(labels, "X-GM-LABELS")
	}
	cmd, err := imap.Wait(client.Client.Fetch(seq, labels...))
	if err != nil {
		return nil, fmt.Errorf("message id search %q: %w", ss, err)
	}

	var res []msg
	for _, rsp := range cmd.Data {
		uid := rsp.MessageInfo().UID

		var labels []string
		if withGmailLabels {
			for _, lbl := range rsp.MessageInfo().Attrs["X-GM-LABELS"].([]imap.Field) {
				labels = append(labels, lbl.(string))
			}
			sort.Strings(labels)
		}

		res = append(res, msg{uid, labels})
	}
	return res, nil
}

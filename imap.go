package main

import (
	"crypto/tls"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mxk/go-imap/imap"
	"golang.org/x/xerrors"
)

const (
	rfc822Attr = "RFC822"
	uidAttr    = "UID"
	labelAttr  = "X-GM-LABELS"
)

type IMAPClient struct {
	*imap.Client
	labels bool
}

func Client(server, email, password, mailbox string) (*IMAPClient, error) {
	tlsCfg := tls.Config{
		InsecureSkipVerify: true,
	}

	cl, err := imap.DialTLS(server, &tlsCfg)
	if err != nil {
		return nil, xerrors.Errorf("creating client: %w", err)
	}

	_, err = cl.Login(email, password)
	if err != nil {
		return nil, xerrors.Errorf("creating client: %w", err)
	}

	if mailbox != "" {
		_, err = cl.Select(mailbox, true)
		if err != nil {
			return nil, xerrors.Errorf("creating client: %w", err)
		}
	}

	go func() {
		// Discard unilateral server data now and then
		time.Sleep(1 * time.Second)
		cl.Data = nil
	}()

	return &IMAPClient{
		Client: cl,
		// Only fetch labels when we're talking to gmail.com
		labels: strings.Contains(server, "gmail.com"),
	}, nil
}

func (client *IMAPClient) GetMail(uid uint32) ([]byte, error) {
	var set = &imap.SeqSet{}
	set.AddNum(uid)

	cmd, err := imap.Wait(client.UIDFetch(set, rfc822Attr))
	if err != nil {
		return nil, xerrors.Errorf("get mail: %w", err)
	}

	resp := cmd.Data[0]
	body := imap.AsBytes(resp.MessageInfo().Attrs[rfc822Attr])

	return body, nil
}

func (client *IMAPClient) Mailboxes() ([]string, error) {
	cmd, err := imap.Wait(client.Client.List("", "*"))
	if err != nil {
		return nil, xerrors.Errorf("list mailboxes: %w", err)
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

func (client *IMAPClient) MsgIDSearch(first, last uint32) ([]msg, error) {
	ss := fmt.Sprintf("%d:%d", first, last)
	seq, _ := imap.NewSeqSet(ss)
	items := []string{uidAttr}
	if client.labels {
		items = append(items, labelAttr)
	}
	cmd, err := imap.Wait(client.Client.Fetch(seq, items...))
	if err != nil {
		return nil, xerrors.Errorf("search message IDs: %w", err)
	}

	var res []msg
	for _, rsp := range cmd.Data {
		uid := rsp.MessageInfo().UID

		var labels []string
		if labelfields, ok := rsp.MessageInfo().Attrs[labelAttr].([]imap.Field); ok {
			for _, lbl := range labelfields {
				labels = append(labels, lbl.(string))
			}
			sort.Strings(labels)
		}

		res = append(res, msg{uid, labels})
	}
	return res, nil
}

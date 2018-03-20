imapchive
=========

Maintain an offline copy of an IMAP account.

Why?
====

 - It's easy to deploy. Download a single binary, add your account
   details to a config file and start syncing. Compatible with macOS,
   Linux and Windows.

 - It's efficient. All mails are stored compressed in a single archive
   file, typically requiring about half the space of what GMail reports
   as your usage.

 - It's safe. All messages are cryptographically hashed to ensure their
   integrity and the archive format is simple, open and documented.
   Messages, once written, are never altered or removed.

 - It's portable. The archive is one self contained file that is easy to
   copy or move. The archive can be exported to a standard format MBOX
   file, readable by most email programs and easily convertible to other
   storage formats.


Archive File Format
===================

The archive file is a simple append-only sequence of records. Each record
start with a four byte, big-endian length and then that many bytes of
data.

     0                   1                   2                   3
     0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
    +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
    |                        Message Length                         |
    +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
    \                                                               \
    /                    Data (variable length)                     /
    \                                                               \
    +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+

The data is a protocol buffer message with the following schema:

  message Record {
      uint32          message_id   = 1;
      bytes           message_data = 2;
      bool            compressed   = 3;
      bytes           message_hash = 4;
      bool            deleted      = 5;
      repeated string labels       = 6;
  }

The fields have the following meaning:

 - `message_id`: A unique integer representing the message, equal to the IMAP message ID.

 - `message_data`: The raw bytes representing the message, in RFC822 format, possibly gzip compressed.

 - `compressed`: True if the `message_data` is in fact gzip compressed.

 - `message_hash`: The SHA256 hash of the (uncompressed) message data.

 - `deleted`: True if the message with this `message_id` has been deleted.

 - `labels`: The set of labels attached to the email (Gmail only).

 A given message ID may be present multiple times in the archive. Since the
 archive is append only this represents the evolution of a message over
 time. Typically the message data does not change, and a record with empty
 data and hash fields indicate that the message data has not changed. The
 labels may however change, and the message may be deleted - indicated by
 the `deleted` flag being set.


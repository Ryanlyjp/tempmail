# Mail Receiving

## MIME parsing

`postfix/mail-receiver.py` reads SMTP input as bytes and parses it with
`email.message_from_bytes`. Keeping the raw bytes until MIME decoding is
required for messages that use `Content-Transfer-Encoding: 8bit`; converting
the complete message to a Python string first can turn UTF-8 body characters
into literal `\uXXXX` sequences.

The original message is still stored in PostgreSQL as UTF-8 text. Invalid
UTF-8 bytes are replaced only when producing that stored raw-text copy; MIME
body decoding always uses the untouched input bytes.

## Deployment check

After rebuilding the Postfix image, verify that
`/usr/local/bin/mail-receiver` inside the running container has the same
SHA-256 hash as `postfix/mail-receiver.py` in the deployment repository.

Historical affected messages must be repaired by parsing each saved
`raw_message` again and updating only `body_text` and `body_html`. Do not use a
global text replacement for `\uXXXX`, because a legitimate message may contain
that sequence as ordinary text.

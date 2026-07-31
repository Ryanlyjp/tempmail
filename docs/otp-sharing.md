# Mailbox OTP Sharing

## Access model

Each share is bound to exactly one mailbox and has one share API key. The same
key is used in both places:

- The browser page is `/otp-share/{api_key}`.
- Programmatic requests use
  `Authorization: Bearer {api_key}`.

Email detail and attachment requests always query by both the requested email
ID and the share's mailbox ID, so a share cannot read another mailbox.

The share API key is stored and returned as plain text, matching TempMail's
existing account API-key management. Protect administrator access and database
backups accordingly.

## Public page

The standalone page can extract the latest OTP, list the most recent five
emails, open an email, extract that email's OTP, and download its attachments.
Extracted OTP values are automatically copied to the clipboard; HTTP pages use
a browser-compatible fallback when the secure Clipboard API is unavailable.

Page requests use these API-key-scoped endpoints:

- `GET /public/otp-share/page/{api_key}/mailbox`
- `GET /public/otp-share/page/{api_key}/latest`
- `GET /public/otp-share/page/{api_key}/emails`
- `GET /public/otp-share/page/{api_key}/emails/{email_id}`
- `GET /public/otp-share/page/{api_key}/emails/{email_id}/otp`
- `GET /public/otp-share/page/{api_key}/emails/{email_id}/attachments/{attachment_id}`

## API-key endpoints

Use the share API key, not an account API key:

```bash
curl -fsSL \
  -H "Authorization: Bearer SHARE_API_KEY" \
  'https://mail.example.com/public/otp-share/latest?format=text'
```

The available API-key endpoints are:

- `GET /public/otp-share/latest`
- `GET /public/otp-share/emails`
- `GET /public/otp-share/emails/{email_id}`
- `GET /public/otp-share/emails/{email_id}/otp`
- `GET /public/otp-share/emails/{email_id}/attachments/{attachment_id}`

## Lifecycle

An administrator can set a finite number of days or permanent validity,
replace the API key, stop or enable the share, and permanently revoke it.
Replacing the API key changes both the page link and API authentication.
Stopped or expired shares reject both page and API access immediately.

Database upgrades use `sql/migrate_v12.sql`. Existing rows keep their current
API key, and the former page-token value is replaced with that API key. This
preserves API clients while intentionally invalidating old page-token links.
The `token` database column remains only as a compatibility mirror and is not
a second credential.

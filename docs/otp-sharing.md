# Mailbox OTP Sharing

## Access model

Each share is bound to exactly one mailbox and has two independent credentials:

- `token` identifies the browser page at `/otp-share/{token}`.
- `api_key` authenticates programmatic requests with
  `Authorization: Bearer {api_key}`.

The page token is not accepted by API-key endpoints. Email detail and
attachment requests always query by both the requested email ID and the
share's mailbox ID, so a share cannot read another mailbox.

The share API key is stored and returned as plain text, matching TempMail's
existing account API-key management. Protect administrator access and database
backups accordingly.

## Public page

The standalone page can extract the latest OTP, list the most recent five
emails, open an email, extract that email's OTP, and download its attachments.

Page requests use these token-scoped endpoints:

- `GET /public/otp-share/page/{token}/mailbox`
- `GET /public/otp-share/page/{token}/latest`
- `GET /public/otp-share/page/{token}/emails`
- `GET /public/otp-share/page/{token}/emails/{email_id}`
- `GET /public/otp-share/page/{token}/emails/{email_id}/otp`
- `GET /public/otp-share/page/{token}/emails/{email_id}/attachments/{attachment_id}`

## API-key endpoints

Use the share API key, not an account API key or page token:

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
replace the page token, replace the API key, stop or enable the share, and
permanently revoke it. Stopped or expired shares reject both page and API-key
access immediately.

Database upgrades use `sql/migrate_v11.sql`. Existing rows retain their page
token and receive empty API-key fields until edited. The SG deployment for
this change intentionally deletes its existing share rows before new shares
are created.

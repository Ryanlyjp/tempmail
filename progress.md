## 2026-07-26 - Task: Fix Unicode MIME body parsing and repair affected emails

### What was done

- Changed SMTP input handling to parse MIME messages from their original bytes so `8bit` UTF-8 text and HTML bodies remain readable.
- Added a regression test based on the Microsoft multipart message structure that previously produced literal `\uXXXX` text.
- Deployed only the Postfix service to SG after creating and validating a complete production backup.
- Reparsed 7 existing affected messages from their saved `raw_message` values and updated only `body_text` and `body_html` in one transaction.

### Testing

- `python3 -m unittest discover -s postfix -p 'test_*.py' -v` passed.
- `python3 -m py_compile postfix/mail-receiver.py postfix/test_mail_receiver.py` passed.
- `git diff --check` passed.
- The local and production Postfix Docker images built successfully.
- Backup SHA-256 checks passed, and the PostgreSQL custom-format dump passed `pg_restore -l` validation inside the production PostgreSQL container.
- Production Postfix is running with zero restarts; its installed receiver script hash matches the deployment repository.
- Production `/health` returned success, and recent API/Postfix logs contained no panic, fatal, or error entries.
- All 7 affected rows passed pre-update old-body checks and post-update reparsed-body checks in the same transaction.
- Production escaped-body count is now 0; the Microsoft sample renders as normal Chinese text and HTML.
- Production counts remained 2 accounts, 59 mailboxes, 223 emails, and 2 OTP shares after repair.

### Notes

- `postfix/mail-receiver.py`: reads SMTP input as bytes and exposes the MIME parsing step for regression testing.
- `postfix/test_mail_receiver.py`: verifies Microsoft-style `8bit` UTF-8 multipart bodies do not become literal Unicode escapes.
- `docs/mail-receiving.md`: documents byte-based MIME parsing, deployment verification, and safe historical repair guidance.
- `progress.md`: records this task, its verification evidence, and rollback procedure.
- Production rollback point: `/opt/tempmail/backup/20260726-pre-mime-unicode-fix`.
- To restore the repaired body fields, run `docker exec -i tempmail-postgres-1 psql -U tempmail -d tempmail < /opt/tempmail/backup/20260726-pre-mime-unicode-fix/affected-emails-before-repair.sql` on SG.
- To restore the previous receiver, extract `./postfix/mail-receiver.py` from the backup `repo.tar.gz` into `/opt/tempmail/repo`, then run `docker compose build postfix && docker compose up -d --no-deps postfix` from `/opt/tempmail/repo`.

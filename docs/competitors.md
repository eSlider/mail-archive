# Competitors — Open Source Email Archive & Webmail Projects

Similar open-source projects with UI for email archival, backup, or webmail. Sorted by relevance to this project (multi-user email archival with sync, search, and web UI).

_Last updated: February 2026_

---

## 1. Email Archiving Systems (Closest Match)

Projects that sync, store, and provide searchable web UI over archived emails.

| Project                  | GitHub                                                                        | Stars | Stack                                                   | License  | Notes                                                                                         |
| ------------------------ | ----------------------------------------------------------------------------- | ----- | ------------------------------------------------------- | -------- | --------------------------------------------------------------------------------------------- |
| **OpenArchiver**         | [LogicLabs-OU/OpenArchiver](https://github.com/LogicLabs-OU/OpenArchiver)     | 1.7k  | TypeScript, SvelteKit, Node.js, PostgreSQL, Meilisearch | AGPL-3.0 | Legally compliant archiving; Google Workspace, M365, IMAP; eDiscovery, RBAC, S3/local storage |
| **Bichon**               | [rustmailer/bichon](https://github.com/rustmailer/bichon)                     | 1.4k  | Rust, TypeScript, Tantivy                               | AGPL-3.0 | Lightweight archiver with WebUI; IMAP, OAuth2; compression, full-text search                  |
| **Daygle Mail Archiver** | [daygle/daygle-mail-archiver](https://github.com/daygle/daygle-mail-archiver) | ~1    | Python                                                  | MIT      | IMAP, Gmail, Office 365; .eml storage, full-text search, retention policies, ClamAV, RBAC     |
| **SimpleMailArchive**    | [ax-meyer/SimpleMailArchive](https://github.com/ax-meyer/SimpleMailArchive)   | 33    | C#, Blazor Server, SQLite                               | GPL-3.0  | IMAP backup to .eml; browsable web UI; Docker support                                         |
| **Archiveium**           | [archiveium/archiveium](https://github.com/archiveium/archiveium)             | 6     | TypeScript, Svelte, PostgreSQL, Minio, Redis            | AGPL-3.0 | Gmail, Outlook, Zoho; early stage; Docker Compose                                             |

---

## 2. Webmail Clients (Read/Send, Often Multi-Account)

Traditional webmail clients; some archive via connection to upstream servers rather than local .eml storage.

| Project        | GitHub                                                                    | Stars | Stack               | License  | Notes                                                                      |
| -------------- | ------------------------------------------------------------------------- | ----- | ------------------- | -------- | -------------------------------------------------------------------------- |
| **Roundcube**  | [roundcube/roundcubemail](https://github.com/roundcube/roundcubemail)     | 6.8k  | PHP                 | GPL-3.0  | Leading webmail; IMAP/SMTP; plugin ecosystem                               |
| **Mailpile**   | [mailpile/Mailpile](https://github.com/mailpile/Mailpile)                 | 8.8k  | Python, JS          | Other    | Privacy-focused; PGP; local web UI; Python 3 rewrite in progress (Moggie)  |
| **RainLoop**   | [RainLoop/rainloop-webmail](https://github.com/RainLoop/rainloop-webmail) | 4.1k  | PHP, JS             | MIT      | **Archived**; no DB; lightweight; SnappyMail is active fork                |
| **SnappyMail** | [the-djmaze/snappymail](https://github.com/the-djmaze/snappymail)         | 1.5k  | PHP                 | AGPL-3.0 | RainLoop fork; modern, fast; PGP, Sieve, dark mode; no social integrations |
| **Cypht**      | [cypht-org/cypht](https://github.com/cypht-org/cypht)                     | 1.4k  | PHP, JS             | LGPL-2.1 | Aggregator; IMAP/SMTP, JMAP, EWS; plugin-based; news-reader style          |
| **Kurrier**    | [kurrier-org/kurrier](https://github.com/kurrier-org/kurrier)             | 847   | TypeScript, Next.js | Other    | Workspace: email, calendar, contacts, storage; IMAP/SMTP                   |
| **MultiEmail** | [MultiEmail/frontend](https://github.com/MultiEmail/frontend)             | —     | —                   | MIT      | Manage multiple accounts; send/receive; customizable desktop notifications |

---

## 3. Static HTML / Lightweight Archive Tools

Backup to browsable static HTML; minimal or no backend server.

| Project                | GitHub                                                                              | Stars | Notes                                       |
| ---------------------- | ----------------------------------------------------------------------------------- | ----- | ------------------------------------------- |
| **imap-to-local-html** | [xtsimpouris/imap-to-local-html](https://github.com/xtsimpouris/imap-to-local-html) | ~3    | Python; IMAP → HTML archive; GPL-3.0        |
| **BackupMailToHTML**   | [dserv01/BackupMailToHTML](https://github.com/dserv01/BackupMailToHTML)             | —     | IMAP → HTML; attachments; backup focus      |
| **NoPriv**             | [RaymiiOrg/NoPriv](https://github.com/RaymiiOrg/NoPriv)                             | —     | Archived; predecessor to imap-to-local-html |

---

## 4. CLI / Migration Tools (No Web UI)

Relevant for sync/backup logic; no built-in web interface.

| Project         | GitHub                                                              | Stars | Notes                                                           |
| --------------- | ------------------------------------------------------------------- | ----- | --------------------------------------------------------------- |
| **imapsync**    | [imapsync/imapsync](https://github.com/imapsync/imapsync)           | 3.9k  | IMAP migration/backup; Perl; widely used; online service exists |
| **imap-backup** | [joeyates/imap-backup](https://github.com/joeyates/imap-backup)     | 1.7k  | Ruby; IMAP backup; CLI setup; JSON storage                      |
| **imapchive**   | [calmh/imapchive](https://github.com/calmh/imapchive)               | 20    | Go; compressed archive; SHA256 integrity; MBOX export           |
| **imapdump**    | [das-kaesebrot/imapdump](https://github.com/das-kaesebrot/imapdump) | —     | Python; IMAP dump to local folder                               |

---

## Comparison Summary

| Capability       | This Project | OpenArchiver | Bichon | Roundcube | Mailpile |
| ---------------- | ------------ | ------------ | ------ | --------- | -------- |
| Multi-user       | ✓            | ✓            | —      | ✓         | —        |
| .eml storage     | ✓            | ✓            | ✓      | —         | —        |
| IMAP sync        | ✓            | ✓            | ✓      | ✓         | ✓        |
| Gmail API        | ✓            | ✓            | ✓      | —         | —        |
| Full-text search | ✓            | ✓            | ✓      | ✓         | ✓        |
| Vector search    | ✓            | —            | —      | —         | —        |
| OAuth2           | ✓            | ✓            | ✓      | —         | —        |
| Self-hosted      | ✓            | ✓            | ✓      | ✓         | ✓        |
| Read-only sync   | ✓            | —            | —      | —         | —        |

---

## References

- [OpenArchiver Demo](https://demo.openarchiver.com)
- [SnappyMail](https://snappymail.eu)
- [Roundcube](https://roundcube.net)
- [Mailpile](https://mailpile.is)
- [Cypht](https://cypht.org)
- [Kurrier](https://www.kurrier.org)
- [Archiveium](https://archiveium.com)

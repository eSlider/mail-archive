# PST/OST Import

## Purpose

Imports Outlook PST and OST files into the archival system. Supports archived mail, desktop migrations, and Exchange/M365 cached copies. All items land under the standard hierarchy: `users/{uuid}/{domain}/{local}/`.

| Item type          | Format | Notes                          |
| ------------------ | ------ | ------------------------------ |
| Email messages     | `.eml` | From, To, Subject, Date, Body  |
| Contacts           | `.vcf` | vCard 3.0                      |
| Calendar events    | `.ics` | iCalendar (VEVENT)             |
| Notes (sticky)     | `.txt` | Subject + body as plain text   |

Import runs with `go-pst` first; on failure (e.g. newer OST, btree bugs), it falls back to `readpst` from `pst-utils` when available.

---

## Todos

- [ ] **Tasks** — Export Outlook tasks to a parseable format (text or JSON)
- [ ] **Journal entries** — Support journal items (rare in recent Outlook)
- [ ] **RFC822 fidelity** — Preserve full headers and MIME; current `.eml` output is simplified
- [ ] **Attachments** — Extract and store instead of omitting
- [ ] **`readpst` fallback** — Document or automate `pst-utils` for CI/containers (OST often requires it)

---

## Ideas

- **Resumable import** — Checkpoint progress and resume after interruption on large files
- **Search integration** — Index `.ics` and `.vcf` in the per-user search layer
- **Duplicate detection** — Apply SHA-256 checksums (as with `.eml`) to skip unchanged items
- **Streaming extraction** — Stream instead of loading whole PST for very large archives

---

## Reference: PST/OST contents

| Component           | PST? | OST? | Notes                                      |
| ------------------- | ---- | ---- | ------------------------------------------ |
| Messages (emails)   | ✓    | ✓    | Inbox, sent, drafts, custom folders        |
| Contacts            | ✓    | ✓    | Address book entries                        |
| Calendars           | ✓    | ✓    | Appointments, meetings, events             |
| Tasks, Notes, Journal | ✓ | ✓  | Outlook-specific item types                 |
| Account settings   | ✗    | ✗    | Stored in registry/profiles, not in file   |

PST = local/archive for POP3/IMAP or manual backup. OST = cached copy for Exchange/M365/Outlook.com.

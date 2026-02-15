### PST and OST Files in Outlook

Both PST (Personal Storage Table) and OST (Offline Storage Table) files store local copies of Outlook data. PST files are used for non-Exchange accounts (e.g., POP3, IMAP) and can serve as archives. OST files are used for Exchange, Microsoft 365, or Outlook.com accounts in cached/offline mode, syncing with the server.

| Component                       | Included in PST? | Included in OST? | Notes                                                                                                               |
| ------------------------------- | ---------------- | ---------------- | ------------------------------------------------------------------------------------------------------------------- |
| Messages (Emails)               | Yes              | Yes              | All folders, including inbox, sent, drafts.                                                                         |
| Contacts                        | Yes              | Yes              | Address book entries.                                                                                               |
| Calendars (Appointments/Events) | Yes              | Yes              | Meetings, events, reminders.                                                                                        |
| Accounts (Settings)             | No               | No               | Account configurations (e.g., server details, credentials) are stored in Outlook profiles/registry, not data files. |

Both also include tasks, notes, and journal entries. OST files are server-synced and not manually backed up like PST.

---

**Data types stored in both PST and OST files** (Microsoft Outlook):

- **Email messages** — including inbox, sent items, drafts, deleted items, and custom folders (with attachments).
- **Contacts** — address book entries.
- **Calendar items** — appointments, meetings, events, reminders.
- **Tasks** — to-do items and task lists.
- **Notes** — sticky-note style entries.
- **Journal entries** — activity tracking (less common in recent versions).

These apply equally to PST (local/archive storage for POP3/IMAP accounts or manual archives) and OST (cached offline copy for Exchange/Microsoft 365/IMAP/Outlook.com accounts synced with server).

**Not included**:

- Account settings/configurations (server addresses, passwords, profiles — stored in Windows registry/Outlook profile files).
- Rules, signatures, or templates (stored separately).
- Add-in data or custom forms (may be external).

Sources: Official Microsoft documentation and consistent technical references confirm this list without additional major item types.

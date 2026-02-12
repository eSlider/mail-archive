"""Send test emails to the local GreenMail SMTP server for testing the sync pipeline."""

from __future__ import annotations

import smtplib
import sys
import time
from email.mime.multipart import MIMEMultipart
from email.mime.text import MIMEText
from email.utils import formatdate

TEST_EMAILS = [
    {
        "subject": "Weekly team standup notes",
        "body": "Hi team,\n\nHere are the notes from today's standup.\n\n- Alice: working on auth module\n- Bob: fixing deployment pipeline\n- Carol: reviewing PRs\n\nBest,\nManager",
        "from": "manager@company.com",
    },
    {
        "subject": "Your order has shipped",
        "body": "Dear customer,\n\nYour order #12345 has been shipped and will arrive in 3-5 business days.\n\nTrack your package: https://example.com/track/12345\n\nThank you!",
        "from": "noreply@shop.example.com",
    },
    {
        "subject": "Re: Project proposal feedback",
        "body": "Thanks for the detailed proposal. I have a few comments:\n\n1. Budget looks reasonable\n2. Timeline might be tight for phase 2\n3. Let's discuss resource allocation on Monday\n\nCheers,\nDavid",
        "from": "david@partner.org",
    },
    {
        "subject": "GitHub notification: PR #42 merged",
        "body": "Pull request #42 'Add email sync module' has been merged into main.\n\nFiles changed: 5\nInsertions: +320\nDeletions: -12",
        "from": "notifications@github.com",
    },
    {
        "subject": "Meeting reminder: Sprint planning tomorrow 10am",
        "body": "This is a reminder that sprint planning is scheduled for tomorrow at 10:00 AM.\n\nPlease prepare your backlog items.\n\nLocation: Conference Room B / Zoom link: https://zoom.us/j/123456",
        "from": "calendar@company.com",
    },
]

SMTP_HOST = "localhost"
SMTP_PORT = 3025
TO_ADDR = "test@greenmail.local"


def send_test_emails() -> int:
    """Send test emails to the GreenMail SMTP server. Returns count of sent messages."""
    sent = 0
    for i, mail in enumerate(TEST_EMAILS):
        msg = MIMEMultipart()
        msg["From"] = mail["from"]
        msg["To"] = TO_ADDR
        msg["Subject"] = mail["subject"]
        msg["Date"] = formatdate(localtime=True)
        msg["Message-ID"] = f"<test-{i}-{int(time.time())}@seed.local>"
        msg.attach(MIMEText(mail["body"], "plain"))

        try:
            with smtplib.SMTP(SMTP_HOST, SMTP_PORT) as smtp:
                smtp.sendmail(mail["from"], [TO_ADDR], msg.as_string())
            sent += 1
            print(f"  [{sent}/{len(TEST_EMAILS)}] Sent: {mail['subject']}")
        except Exception as e:
            print(f"  FAILED: {mail['subject']} — {e}", file=sys.stderr)

    return sent


if __name__ == "__main__":
    print(f"Sending {len(TEST_EMAILS)} test emails to {TO_ADDR} via {SMTP_HOST}:{SMTP_PORT}...")
    count = send_test_emails()
    print(f"Done. {count}/{len(TEST_EMAILS)} emails sent.")
    sys.exit(0 if count == len(TEST_EMAILS) else 1)

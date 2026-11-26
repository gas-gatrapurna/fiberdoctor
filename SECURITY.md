# Security Policy

## Supported versions

| Version | Supported |
|---------|-----------|
| 0.1.x   | Yes       |

## Reporting a vulnerability

Please report security issues privately via a **GitHub Security Advisory**
(Security -> Advisories -> New draft security advisory) instead of a public
issue.

We aim to acknowledge a report within 72 hours and to ship a fix or mitigation
within 14 days for critical findings.

Please do not disclose details publicly until a fix has been released.

## Scope notes

* `fd_golden` talks to devices over Telnet and enforces a read-only guard:
  only commands starting with `show ` are sent; anything else is rejected
  before the device is touched.
* Credentials are never stored by this tool. Provide them via `FD_OLT_PASS`
  or a credential file referenced by `FD_CRED_VAULT`.
* `fd_golden` writes captured output to disk. Captures contain subscriber
  serial numbers and names - keep them out of version control.

# Security policy

`uspace-core` is a library used by safety-relevant systems. Report a
vulnerability privately to the repository owner through GitHub's private
vulnerability reporting on this repository (Security tab, "Report a
vulnerability"). Do not open a public issue.

We acknowledge a report within 7 days and aim to publish a fix, or an
agreed statement, within 90 days of the report. A fix to a judgement that
changes behaviour ships as a major version per spec `00 §6.3`, with the
vector that pins the corrected behaviour.

Scope: everything in this module. Out of scope: the systems that import it
(each has its own policy) and the lab simulators.

This repository is public. No secret, key, certificate or token is ever
committed, including test keys; `gitleaks` runs in CI. Test keys for the
`auth` package are generated at test time.

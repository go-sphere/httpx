# Response contracts

One file per conformance case, shared by every adapter: the suite renders the
contractual part of a response and compares it with the file, so the contract
is written down instead of being "whatever one adapter produced".

Format:

```
status: 200
content-type: application/json     # "n/a" when the contract has no body
location: /target                  # only when present
x-trace: value                     # only when present
cookie: name=… value=… path=… …    # one line per cookie, sorted by name
body: json | json-sha256 | text | none | ignored # "ignored" = redirect decoration
---
<body, JSON re-encoded canonically or its SHA-256 digest>
```

What is deliberately *not* here: middleware order, short-circuiting, allocation
counts, lifecycle and streaming timing. Those are assertions about sequences,
and a golden file for a sequence invites regenerating over a real reordering
bug — they stay as inline assertions in the case code.

To rewrite the files after an intended change:

```sh
make golden      # records from every adapter, then verifies all of them
```

Update mode only writes from a source checkout. After rewriting, the same
target runs the suite for every adapter *without* the variable, so a change
that only one adapter agrees with still fails.

Update mode never deletes a file. When every case ran and passed, `Run` ends
with a `GoldenFilesInUse` subtest that fails on any file here (other than this
README) that no case compared and no skipped case covers, so a renamed or
deleted case's file has to be removed by hand. Cases skip through the runner's
`skipf`, which records the skip; `TestCasesSkipThroughRunner` enforces that.

Text contracts include `body-bytes` before the separator so the formatting
newline at EOF cannot hide a changed response newline. JSON numbers retain
all digits. Canonical JSON larger than 16 KiB uses `json-sha256`; its digest
checks the entire value while keeping the contract small enough to review.

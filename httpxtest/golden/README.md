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
body: json | text | none | ignored # "ignored" = redirect decoration
---
<body, JSON re-encoded canonically>
```

What is deliberately *not* here: middleware order, abort behavior, allocation
counts, lifecycle and streaming timing. Those are assertions about sequences,
and a golden file for a sequence invites regenerating over a real reordering
bug — they stay as inline assertions in the case code.

To rewrite the files after an intended change:

```sh
make golden      # records from every adapter, then verifies all of them
```

Update mode only writes from a source checkout. After rewriting, the same
target runs the suite for all four adapters *without* the variable, so a change
that only one adapter agrees with still fails.

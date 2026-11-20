# Fixture provenance

These JSON fixtures are adapted from the Apache-2.0 licensed
[ByteDance Sonic benchmark data](https://github.com/bytedance/sonic) at commit
`4640e45556c461204b438560cdb0ee80ad225790`.

- `small.json` comes from `testdata/small.go` (`Book`, 365 bytes).
- `medium.json` comes from `decoder/testdata_test.go` (`TwitterJson`). Twelve
  upstream space-only blank lines are trimmed for Git whitespace checks; the
  normalized fixture is 13,437 bytes.
- `large.json` is an unmodified copy of `testdata/twitter.json` (646,995
  bytes, 100 statuses).

Sonic is licensed under the Apache License 2.0. A copy is stored in
[`LICENSE-APACHE-2.0`](LICENSE-APACHE-2.0).

# Security

eggbot is an IRC client. Report vulnerabilities privately to the repository owner ([SECURITY.md](../SECURITY.md)). Do not open a public issue for credential leaks, remote code execution, or authentication bypass.

Privilege is resolved in this order: services account, certificate fingerprint, then a configured hostmask. Nickname alone is not a privilege.

IRC passwords and SASL require verified TLS. A plaintext partyline is accepted only on loopback. Remote administration needs `partyline.tls_listen` and a certificate.

Each outbound IRC message is a single framed line, so model or script output cannot inject a second command via newline. `log.debug_irc` is rejected because it can record `/msg eggbot pass …`. The SQLite file is created mode `0600`. DCC is optional and does not dial private or loopback addresses.

Python plugins run as a separate OS process with access to the filesystem and network. They are disabled until `scripts.python = true` and the script is listed in `scripts.trusted`.

LLM actions that change channel state wait for `.confirm`. Channel text used as model context is kept in memory and discarded when the process exits. Password commands are not written to logs, scripts, seen, quotes, or model history.

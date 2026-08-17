# IRCv3

eggbot requests these capabilities only when the server advertises them. An empty `CAP LS` completes registration without `CAP REQ`.

## Requested when advertised

- CAP 302 / cap-notify
- sasl (only if SASL is configured)
- account-tag, account-notify
- away-notify
- chghost
- extended-join
- invite-notify
- message-tags
- multi-prefix
- server-time
- setname
- userhost-in-names
- batch

## Not requested

- echo-message (would duplicate command handling and LLM memory)
- draft/multiline, draft/chathistory, read markers, reactions, typing

## Privilege

1. SASL / services account bound in the userfile
2. Certificate fingerprint
3. Configured hostmask

Nickname alone is not a privilege.

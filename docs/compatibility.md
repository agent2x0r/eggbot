# Networks

One IRC network per process. `server.endpoints` lists additional addresses for that network when the first address is unavailable.

| Daemon | Notes |
|--------|--------|
| Ergo | TLS, SASL, CAP 302, account tags |
| InspIRCd | PREFIX and CHANMODES vary |
| UnrealIRCd | SASL, cloaks, extra prefixes |
| Solanum | charybdis-family ISUPPORT |
| chonkline | Short 353/332/366 numerics; empty CAP LS; NOTICE is supported |

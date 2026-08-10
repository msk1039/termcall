# Third-party notices

TermCall uses Go modules and embeds their code into released binaries. Every
release includes `THIRD_PARTY_NOTICES.txt`, generated from the exact module graph
used for that release, plus an SPDX SBOM. Consult each listed component's source
and license for its terms; those licenses continue to apply.

To regenerate the inventory locally:

```sh
./scripts/generate_third_party_notices.sh THIRD_PARTY_NOTICES.txt
```

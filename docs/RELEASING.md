# Releasing

One tag produces one release: a signed, notarized, stapled universal disk image,
installable with `brew install`.

```sh
brew install antimatter-studios/tap/snapshotter
```

## What happens

```
git tag v0.2.0 && git push origin v0.2.0
        │
        ▼
.github/workflows/release.yml   on macos-15
        │
        ├─ universal build (lipo: arm64 + amd64, both verified present)
        ├─ codesign  --options runtime --timestamp --entitlements …
        ├─ hdiutil   Snapshotter_0.2.0_universal.dmg   (signed too)
        ├─ notarytool submit --wait   →   stapler staple
        ├─ spctl --assess             (the check Gatekeeper itself runs)
        └─ gh release create          + SHA256SUMS
        │
        ▼
antimatter-studios/homebrew-tap  →  run `tap-sync` (manual)
        └─ downloads the asset, computes its sha256, rewrites Casks/snapshotter.rb
```

The tap **pulls**. This repository never pushes to it and holds no token that
could, which is why adding a release does not widen what a compromised workflow
here could reach.

## Required secrets

All are repository secrets. The workflow fails on the first step if the signing
ones are absent, rather than publishing something macOS will refuse to open.

| Secret | What it is |
| --- | --- |
| `APPLE_CERTIFICATE` | Developer ID Application certificate **and private key**, exported as `.p12`, base64-encoded |
| `APPLE_CERTIFICATE_PASSWORD` | the password set when exporting that `.p12` |
| `APPLE_SIGNING_IDENTITY` | the identity's full name, e.g. `Developer ID Application: Name (TEAMID)` |
| `KEYCHAIN_PASSWORD` | any throwaway string; unlocks the temporary keychain the job creates |
| `APPLE_ID` | the Apple ID that owns the developer account |
| `APPLE_PASSWORD` | an **app-specific password**, not the Apple ID password — appleid.apple.com → Sign-In and Security → App-Specific Passwords |
| `APPLE_TEAM_ID` | the 10-character team ID, the parenthesised part of the identity |

Exporting the certificate:

```sh
security find-identity -v -p codesigning          # confirm which one you have
# Keychain Access → My Certificates → the Developer ID Application entry →
# right-click → Export → .p12 → set a password
base64 -i Certificates.p12 | pbcopy               # paste as APPLE_CERTIFICATE
```

Export the **certificate with its private key** (expand the certificate row and
select the pair). A certificate alone imports without error and then fails at
`codesign` with "no identity found", which reads as a wrong identity name.

## Before tagging

`CHANGELOG.md` must have a section for the version. The release notes are read
from it, and the `git-changelog` pre-push guard refuses a tag whose version is
undocumented — so this is enforced twice, on purpose.

## Notarization is not optional

A cask applies macOS's quarantine attribute to what it installs, and Gatekeeper
refuses to open a quarantined bundle that is not notarized — reporting it as
"damaged", which sends users looking for a corrupt download rather than a missing
signature. Notarization additionally requires the hardened runtime and a secure
timestamp; the workflow asserts both are present after signing rather than
discovering it from Apple's rejection.

See [build/darwin/entitlements.plist](../build/darwin/entitlements.plist) for what
the hardened runtime is and is not allowed to do here, and why the app sandbox is
absent.

## What a release still cannot do for the user

Notarization gets the application open. It does not grant **Full Disk Access**,
which mounting a snapshot cannot work without, and which only the user can give in
System Settings. The cask says so in its caveats. Treat that as an install step
rather than a bug report.

## Local signing

Ordinary `task package` builds are signed too, with whatever Developer ID is in
the keychain — discovered, not configured, so there is nothing to remember. This
matters beyond tidiness: an ad-hoc signature is identified by a hash of the build,
so every rebuild looks like a different application and silently voids the Full
Disk Access grant. A machine with no Developer ID falls back to ad-hoc and says
so. `SNAPSHOTTER_SIGN_IDENTITY` overrides the lookup if a specific certificate is
needed.

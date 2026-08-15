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
        ├─ notarize the APP   → stapler staple Snapshotter.app
        ├─ hdiutil   Snapshotter_0.2.0_universal.dmg   (built around the stapled app)
        ├─ notarize the IMAGE → stapler staple the .dmg
        ├─ spctl --assess             (the check Gatekeeper itself runs)
        └─ gh release create          .dmg + SHA256SUMS
        │
        ▼
antimatter-studios/homebrew-tap  →  run `tap-sync` (manual)
        └─ downloads the asset, computes its sha256, rewrites Casks/snapshotter.rb
```

The tap **pulls**. This repository never pushes to it and holds no token that
could, which is why adding a release does not widen what a compromised workflow
here could reach.

## Required secrets

Seven repository secrets. They exist but are **empty** until filled in, and an
empty one fails the workflow's first step deliberately: publishing an unsigned or
un-notarized build is worse than not publishing, because a cask installs it,
quarantines it, and macOS then reports it as "damaged".

Set them at **Settings → Secrets and variables → Actions**, or with `gh secret
set NAME` (which prompts, so the value never lands in shell history).

### 1. `APPLE_TEAM_ID`

The 10-character team ID — the parenthesised part of your signing identity.

```sh
security find-identity -v -p codesigning
#   1) ABC…  "Developer ID Application: Your Name (43UMKXZ8P4)"
#                                                  ^^^^^^^^^^ this
```

### 2. `APPLE_SIGNING_IDENTITY`

That identity's **full name**, exactly as printed above, including the
parentheses:

```
Developer ID Application: Your Name (43UMKXZ8P4)
```

It must be a *Developer ID Application* certificate. An *Apple Development* one
signs fine locally and is rejected by notarization.

### 3. `APPLE_CERTIFICATE`

The certificate **and its private key**, as a base64-encoded `.p12`.

```sh
# Keychain Access → My Certificates → expand the "Developer ID Application" row
# → select the certificate AND the key beneath it → right-click → Export…
# → format: Personal Information Exchange (.p12) → set a password
base64 -i Certificates.p12 | pbcopy
```

Export the pair, not the certificate alone. A certificate on its own imports
without complaint and then fails at `codesign` with "no identity found", which
reads as a wrong identity name and sends you looking in the wrong place.

### 4. `APPLE_CERTIFICATE_PASSWORD`

The password you typed during that export. Nothing else uses it.

### 5. `KEYCHAIN_PASSWORD`

Any throwaway string — it only unlocks the temporary keychain the job creates and
discards. `uuidgen` is a fine source.

### 6. `APPLE_ID`

The Apple ID that owns the developer account, as an email address.

### 7. `APPLE_PASSWORD`

An **app-specific password**, not your Apple ID password. Notarization refuses the
account password outright.

> appleid.apple.com → Sign-In and Security → App-Specific Passwords → generate

Store the generated `xxxx-xxxx-xxxx-xxxx` value.

## Before tagging

`CHANGELOG.md` must have a section for the version. The release notes are read
from it, and the `git-changelog` pre-push guard refuses a tag whose version is
undocumented — so this is enforced twice, on purpose.

## One artefact, one binary

A release publishes a disk image and nothing else. The application and the
command line are the same executable inside it, so there is no second thing to
build, sign, notarize or keep in step.

It was briefly published as a standalone `.tar.gz` as well, for a companion
formula that no longer exists. Copying that binary out of the bundle meant
re-signing it — an executable inside a bundle is signed *as part of* that bundle,
its signature sealing the Info.plist beside it, so a copy fails verification with
"invalid Info.plist (plist or signature have been modified)" and macOS refuses to
run it. Not shipping a copy removes that whole problem rather than working around
it.

The cask does both jobs:

```sh
brew install --cask antimatter-studios/tap/snapshotter
```

It stages the application and symlinks the bundle's own executable onto PATH.
That was once refused by the tap's CI, which rejected any cask carrying a
`binary` stanza — a rule written for casks that ship a *bare* binary, where
Homebrew's quarantine kills an ad-hoc-signed executable on first run. A
notarized, stapled, macOS-only bundle has no such problem, and the rule now
fails only a cask that has a binary and no app.

Symlinking rather than installing a second copy is not just tidier. macOS
attributes Full Disk Access to the executable making the call, so a separate
copy would need its own grant before `snapshotter browse` could mount anything.

The command line is not offered without the application. It is the application's
own executable, and the work people reach for it to do — browsing a snapshot,
restoring from one — mounts a filesystem, which needs the Full Disk Access grant
that only the installed bundle can carry.

## Why both the application and the image are stapled

Two notarization submissions, and the order matters.

A ticket is stapled to one specific thing. Stapling only the disk image leaves the
bundle inside it without one — and a cask does not install the image, it copies the
application out of it. That copy would have no ticket, so Gatekeeper would have to
ask Apple on first launch: fine with a network, and refusable without one. That is
precisely the machine this application exists to rescue, so the application is
notarized and stapled first, and the image is built around the stapled copy.

The image is then notarized and stapled in its own right, because Gatekeeper also
assesses the file that was downloaded, not only what is inside it.

Five checkpoints assert this rather than assuming it: the staple on the application,
the staple surviving into the copy that goes into the image, the staple on the image,
and `spctl` plus `stapler validate` against both after the image is mounted. The
last of those was briefly wrong — it asserted a ticket on the inner bundle while only
the image was being stapled, and failed a release whose artifact was perfectly good.
It is correct now because the thing it asserts is finally true.

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

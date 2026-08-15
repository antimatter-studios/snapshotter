# The Homebrew formula for the snapshotter command line tool.
#
# Separate from the cask because this tap ships command-line tools as formulae —
# a cask that symlinks a binary onto PATH is refused by its own CI, which is the
# same split trove uses: trove-cli alongside trove-desktop.
#
# It is the same binary the application bundle contains, taken from inside it at
# release time and re-signed as a standalone executable. So installing both puts
# one program on the machine twice rather than two programs that might disagree,
# and `snapshotter version` reports the same value the window shows.
#
# version and sha256 are owned by tap-sync, which reads projects.json, downloads
# the declared asset and computes the digest itself. Do not edit them by hand.
class SnapshotterCli < Formula
  desc "Browse, compare and restore APFS local snapshots from the command line"
  homepage "https://github.com/antimatter-studios/snapshotter"
  version "0.0.0"
  license "MIT"

  # One universal archive rather than a per-architecture pair: the binary inside
  # is already both, so splitting it would mean shipping the same file twice.
  url "https://github.com/antimatter-studios/snapshotter/releases/download/v#{version}/snapshotter_#{version}_darwin_universal.tar.gz"
  sha256 "0000000000000000000000000000000000000000000000000000000000000000"

  depends_on :macos
  depends_on macos: :monterey

  def install
    bin.install "snapshotter"
  end

  def caveats
    <<~EOS
      `snapshotter` reads and takes snapshots without any special permission, so
      list, status, take and run work immediately.

      Opening a snapshot to browse or restore from it is different: that mounts a
      filesystem, which needs an administrator password AND Full Disk Access
      granted to the thing making the call. Install the application for that:

        brew install --cask antimatter-studios/tap/snapshotter

      Snapshots are not a backup. They live on the same disk as your data and
      protect against deletion, not against the disk failing.
    EOS
  end

  test do
    # Proves the binary runs and is the version Homebrew thinks it installed —
    # which is the thing most likely to be wrong after a release, since the
    # version is stamped at build time rather than read from anywhere.
    assert_match version.to_s, shell_output("#{bin}/snapshotter version")
    assert_match "snapshotter", shell_output("#{bin}/snapshotter help")
  end
end

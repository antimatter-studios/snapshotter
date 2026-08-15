# The Homebrew cask for Snapshotter.
#
# This copy is the source of truth for the *shape* of the cask. The copy that
# users install is Casks/snapshotter.rb in antimatter-studios/homebrew-tap, and
# the tap's own tap-sync workflow rewrites the version and sha256 there from
# whatever the release actually contains. So:
#
#   * edit this file to change what the cask DOES, then copy it to the tap;
#   * never hand-edit version or sha256 in either copy — tap-sync owns those.
#
# The version and sha256 below are placeholders for exactly that reason.
cask "snapshotter" do
  version "0.0.0"
  sha256 :no_check

  url "https://github.com/antimatter-studios/snapshotter/releases/download/v#{version}/Snapshotter_#{version}_universal.dmg"
  name "Snapshotter"
  desc "Local restore points for macOS without a backup drive"
  homepage "https://github.com/antimatter-studios/snapshotter"

  livecheck do
    url :url
    strategy :github_latest
  end

  # LSMinimumSystemVersion in the bundle says 12.0.
  depends_on macos: ">= :monterey"

  app "Snapshotter.app"

  # The same binary serves the window and the command line, so linking it puts
  # `snapshotter list` / `status` / `take` / `run` on PATH without a second
  # download. It must point inside the installed bundle rather than at a copy:
  # Full Disk Access is granted to the bundle, and a separate copy of the binary
  # would be a different identity with no grant.
  binary "#{appdir}/Snapshotter.app/Contents/MacOS/snapshotter"

  # Uninstalling has to stop the two agents first. Without this, launchd keeps
  # running a binary that is no longer there, which fails every interval forever
  # and leaves the plists behind.
  uninstall launchctl: [
              "com.christhomas.snapshotter",
              "com.christhomas.snapshotter.tripwire",
            ],
            quit:      "com.christhomas.snapshotter"

  # zap, not uninstall: `brew uninstall` leaves every one of these alone, which is
  # why a reinstall keeps your schedule and your settings. Only `brew uninstall
  # --zap` asks for all trace of it to be gone, and then the settings file has to
  # go too — it moved to ~/.config in 0.3.0.
  zap trash: [
    "~/.config/snapshotter",
    "~/Library/Application Support/Snapshotter",
    "~/Library/LaunchAgents/com.christhomas.snapshotter.plist",
    "~/Library/LaunchAgents/com.christhomas.snapshotter.tripwire.plist",
    "~/Library/Logs/snapshotter.log",
    "~/Library/Logs/snapshotter-tripwire.log",
  ]

  caveats <<~EOS
    Snapshotter needs Full Disk Access before it can open a snapshot:

      System Settings -> Privacy & Security -> Full Disk Access -> add Snapshotter

    Mounting a snapshot needs an administrator password as well, once per batch.
    Root alone is not enough — macOS checks Full Disk Access against the
    application making the call, so without the grant every open is refused with
    "Operation not permitted".

    Snapshots are not a backup. They live on the same disk as your data and
    protect against deletion, not against the disk failing.
  EOS
end

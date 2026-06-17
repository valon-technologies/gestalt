# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.19"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.19/gestaltd-macos-arm64.tar.gz"
      sha256 "f949edd6caf38a23e5ea49f4e46f3056375be6524d798d25293d78e922a37a09"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.19/gestaltd-macos-x86_64.tar.gz"
      sha256 "5e507a1dbbf86c440859c606b9ca909ed12a563b20b35df1813ab476621380e8"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.19/gestaltd-linux-arm64.tar.gz"
      sha256 "98bfbadb3ee61e207f93f980430e7bbdefc5ee677a77614d729b9ae347597456"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.19/gestaltd-linux-x86_64.tar.gz"
      sha256 "f8e87fa34d40d71324806ba5a4d478662ad924a877d958a44a670ddcbdacc88f"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end

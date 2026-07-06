# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.38"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.38/gestaltd-macos-arm64.tar.gz"
      sha256 "e0d0ca2775ed9a4ffc8605b9127c7ca8c7d6d68997b4882db0f462616116a5ca"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.38/gestaltd-macos-x86_64.tar.gz"
      sha256 "d1cf156d5142575f7f559cef80a02bb6e430abd453ff54d9f80fac78e75aa931"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.38/gestaltd-linux-arm64.tar.gz"
      sha256 "999844b894c498e90511b5da1d8d72e3a278961791eaa746650a721836e0b68a"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.38/gestaltd-linux-x86_64.tar.gz"
      sha256 "8351e827f6294e30936529d0e1fb0c8a259698ed7171bd6c3a2af3275532183b"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end

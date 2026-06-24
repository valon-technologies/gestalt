# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.30"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.30/gestaltd-macos-arm64.tar.gz"
      sha256 "a67052a0f6dae75b74d46700d94c5a022d7e28c0744fbe2bc113df90a3dce5b8"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.30/gestaltd-macos-x86_64.tar.gz"
      sha256 "9bb446b0a71bb49251d500ff95a765074fb14e1b21d05761dfe9bf1137d00148"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.30/gestaltd-linux-arm64.tar.gz"
      sha256 "be4649ee8d43b8179ec8a5427a89930d2ab5e10ebc8e6a361da988580f2e4354"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.30/gestaltd-linux-x86_64.tar.gz"
      sha256 "f4e6cdcfa24c075e3b1e28d4e417ea06a7d108612b2370a10f01012e8973a32e"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end

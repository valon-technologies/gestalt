# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.18"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.18/gestaltd-macos-arm64.tar.gz"
      sha256 "d4104de57bf3a48d856aedb4c9dd844dddb8c34a445346d33e4bf0e896fd42ad"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.18/gestaltd-macos-x86_64.tar.gz"
      sha256 "f9f89823ffc1ecd0ef498d922483318c69b4e0716a8c11a562de7ed03c12770e"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.18/gestaltd-linux-arm64.tar.gz"
      sha256 "2fdc53a7d1060aff009038715ab91fc404154eb669180528c6e507dacb8ef16d"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.18/gestaltd-linux-x86_64.tar.gz"
      sha256 "3c43711c695b7fbb0c57e111a881695b124958d41120c2f6fb3cb3f3df6ae324"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end

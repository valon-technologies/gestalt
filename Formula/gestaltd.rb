# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.41"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.41/gestaltd-macos-arm64.tar.gz"
      sha256 "cef7948c9d8c557055667f46a4467860e108daa5e73d36b692d76df1bcf955d3"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.41/gestaltd-macos-x86_64.tar.gz"
      sha256 "e6d27451c2d6c888a4308307ee06eb2b3e209c6f63bcbb6cb043458b2a413c78"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.41/gestaltd-linux-arm64.tar.gz"
      sha256 "7ac45deafdf7f8f5d9066d8ec5d9c185edae8fe9170d8516213474234daea996"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.41/gestaltd-linux-x86_64.tar.gz"
      sha256 "d661524a731ad53e8d04863ef906b53ffa978ada5621b67e508a3b4267d51f6a"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end

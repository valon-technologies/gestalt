# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.51"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.51/gestaltd-macos-arm64.tar.gz"
      sha256 "33c62e47b28c5b3c416889640a399f44d59ea59f2da709067f24d259bb084567"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.51/gestaltd-macos-x86_64.tar.gz"
      sha256 "354c920c5a842c6ade94f042be9f87c74962468174a69ad3916cd41e3111a220"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.51/gestaltd-linux-arm64.tar.gz"
      sha256 "bc1c746bdeee99eb885fd5b2909d8b126ea4abeb40dfc436b98b02a052d135bf"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.51/gestaltd-linux-x86_64.tar.gz"
      sha256 "4710c4a71dca1a453d53d26d3f4f2ef04d05b0d1454d3172a07d5ee866346b74"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end

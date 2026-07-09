# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.39"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.39/gestaltd-macos-arm64.tar.gz"
      sha256 "89144ff294cd6935f6dcfd13527ece0b4167f04920f95f6fbf56efc88b67b1d7"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.39/gestaltd-macos-x86_64.tar.gz"
      sha256 "9611ba9ebefd13d62d13f26da505f13a268d40044ce7bcc48e49c740ecf3e4dd"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.39/gestaltd-linux-arm64.tar.gz"
      sha256 "b7ded142977d1743eb39e36fd64dcbfddd13e1bf4d01348fb4abf48f656c6639"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.39/gestaltd-linux-x86_64.tar.gz"
      sha256 "1f3c82fd9d8536dd76f17058e6e342d8699a35f7194f16bdeb9209a64c5acd08"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end

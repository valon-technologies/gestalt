# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.1"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.1/gestaltd-macos-arm64.tar.gz"
      sha256 "32a1c4eebf1f5e795ad7d55c9a36c8768e028e06847aeaacb64e64420204f161"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.1/gestaltd-macos-x86_64.tar.gz"
      sha256 "0a6f9971490ed3f6e662048d8586ebcc3066453075858efdda17aec6b71e2295"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.1/gestaltd-linux-arm64.tar.gz"
      sha256 "a16d54fdc6080e541a656c90f09f30a107d1e40001a772790fa881d06796d5f0"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.1/gestaltd-linux-x86_64.tar.gz"
      sha256 "46d9af66f3c1383500b390608f7702d412cc2ccdd1a602c069514ca26a4569fc"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end

# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.10"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.10/gestaltd-macos-arm64.tar.gz"
      sha256 "9bb040589cf5ebe6843a51b2c1a9ca677a9ce34709703979d0bd51573572abb1"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.10/gestaltd-macos-x86_64.tar.gz"
      sha256 "ba2405a7f9fa2777d0cfd5f2208f3950b2664ab9a2a5102e3f0c1303d7ab9b49"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.10/gestaltd-linux-arm64.tar.gz"
      sha256 "88fc9d3171f1210969644d80f5e5a80931ea3390d011107832b5966c600010b6"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.10/gestaltd-linux-x86_64.tar.gz"
      sha256 "6cd0877e817f90937f6479b75f1aac09232ac45ea0227e131f82772b07f2209c"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end

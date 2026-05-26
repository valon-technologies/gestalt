# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.5"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.5/gestaltd-macos-arm64.tar.gz"
      sha256 "7fcaf6cee442dddfbd86057f2af7dcd7ad21f8e3fea779d13f206d319f14d328"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.5/gestaltd-macos-x86_64.tar.gz"
      sha256 "b6fbc1997c02a90546548f7bd946fab456e467a7469c5a76c70b733ed1616b2e"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.5/gestaltd-linux-arm64.tar.gz"
      sha256 "ffb0b05083876135840b7e06632989d2b70a21c53bed0c76703409628b0fccf5"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.5/gestaltd-linux-x86_64.tar.gz"
      sha256 "a86a3640e6795cb04119fb7b2a5168f9caeb95aa361cc4d3fece6c2e055a56d1"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end

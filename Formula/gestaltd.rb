# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.56"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.56/gestaltd-macos-arm64.tar.gz"
      sha256 "659983aa6d1e2daca4b820162af31e6b9193877e225e4ec8fe637ea96cdaeb21"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.56/gestaltd-macos-x86_64.tar.gz"
      sha256 "0ffb65d0924c58773b2a5ab4c5a7572b35016d9660e9152a89ecae38cbcd7ee7"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.56/gestaltd-linux-arm64.tar.gz"
      sha256 "f4508f33c2d2c83ef7a128a88afa170fb3816951d918bf4691054f08282f555b"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.56/gestaltd-linux-x86_64.tar.gz"
      sha256 "73f40ad7ec4dc9a14b371bc8370fa35d58aadda4399b04b308904be34d6e34bb"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end

# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.2"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.2/gestaltd-macos-arm64.tar.gz"
      sha256 "1c251b9e193c07c5e4190d1d7b3b8a91d4bd70a81f320bb6296312e934658dfd"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.2/gestaltd-macos-x86_64.tar.gz"
      sha256 "ff6a1094e60627a49731059cb503d69197ce6c4c663c30fb6ad0745e3208ccda"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.2/gestaltd-linux-arm64.tar.gz"
      sha256 "356a988624654b0475f3eeb852a91e79b1e02d99d242fabc4ef850dcc2e8ae73"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.2/gestaltd-linux-x86_64.tar.gz"
      sha256 "8900dbaadf3b03b391376e4997e9ddee5fe4a043622633b251ba4657d3cc7687"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end

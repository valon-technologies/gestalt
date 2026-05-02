# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.11"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.11/gestaltd-macos-arm64.tar.gz"
      sha256 "4c826b95d518b59a780f9c2003800ba7265458efbbb284ad054c6eacaed5a8ee"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.11/gestaltd-macos-x86_64.tar.gz"
      sha256 "19cd284d2a0edcae13a88a09cba3fe95a676166140fec7541cc0d2740f38d7d4"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.11/gestaltd-linux-arm64.tar.gz"
      sha256 "4fa112e2e5668e06765a402c2344ea22ad67a05acc21ba6e3126ca457c68cd17"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.11/gestaltd-linux-x86_64.tar.gz"
      sha256 "205851f49785b5949db4576bed2863c9f9a253503a8059ad44d1320614560544"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end

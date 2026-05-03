# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.12"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.12/gestaltd-macos-arm64.tar.gz"
      sha256 "43ad06bd1f49980a91e7e0790ec5faa0e465eef5eb1e790694481f041086eabc"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.12/gestaltd-macos-x86_64.tar.gz"
      sha256 "a56aa4804babc812510155d8aabf1bc3abd13d1ff6a95a4fd80783c529fdf78c"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.12/gestaltd-linux-arm64.tar.gz"
      sha256 "d7ca1b847411b37e303b0c42865c21ad310aa05974ed17966dd2e8692a5f527a"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.12/gestaltd-linux-x86_64.tar.gz"
      sha256 "1f89299c8dab4a0e907d25066dade186c83d6a98ed563bc23cf1f0db4e6b7e65"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end

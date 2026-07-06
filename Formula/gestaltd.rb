# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.38"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.38/gestaltd-macos-arm64.tar.gz"
      sha256 "0105f9bc2c891e55d478733727239c5bd851fb2d3eb0af66fb130d4bcd424316"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.38/gestaltd-macos-x86_64.tar.gz"
      sha256 "c5ae5992c5c234e4afbd87bb53ad8d86aa35a38726ac70cced166dabd6d00681"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.38/gestaltd-linux-arm64.tar.gz"
      sha256 "42c44b5ad1831bbfe9706ae36feafef571cd80ffefc848c67511c6160d5b6f86"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.38/gestaltd-linux-x86_64.tar.gz"
      sha256 "d3eac496f0954e32178c8b8a59bf0b40821203c1e98d48e8e99b206249577dfc"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end

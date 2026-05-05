# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.15"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.15/gestaltd-macos-arm64.tar.gz"
      sha256 "eb208c22f7f6be2ca8706f27b89c45076bd8cb81fb9f88d442c11bb8b320ad00"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.15/gestaltd-macos-x86_64.tar.gz"
      sha256 "e93e495e123b09dd290e9011920a7c91f6541c04ed4785c685d4ac48c8df08ac"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.15/gestaltd-linux-arm64.tar.gz"
      sha256 "79acb403279842f400333586e31a09c5223748da5d185161812d097540880333"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.15/gestaltd-linux-x86_64.tar.gz"
      sha256 "6e09de8f417ddcbd0b13c0c6b504fcea1ca4cd61ad938cb66539ba78b6007fe3"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end

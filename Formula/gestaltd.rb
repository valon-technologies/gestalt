# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.41"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.41/gestaltd-macos-arm64.tar.gz"
      sha256 "263d96a5586542661d9f770b2730045df36cd40c33b1efbe119cdec33ad8eaa6"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.41/gestaltd-macos-x86_64.tar.gz"
      sha256 "014a8adb50978658088c7d12a81b20b137ad54b8dedd6475a895b4d3784cb9e2"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.41/gestaltd-linux-arm64.tar.gz"
      sha256 "7cd624bfbad43d75f31cd6f5170840bde47f8c186aa81999ced267d12d4ec846"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.41/gestaltd-linux-x86_64.tar.gz"
      sha256 "c478f7d85a090d64305df8417f1b6325acba9b5ae9898bae4ed52e4ff83828a8"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end

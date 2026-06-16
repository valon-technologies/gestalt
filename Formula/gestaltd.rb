# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.18"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.18/gestaltd-macos-arm64.tar.gz"
      sha256 "4348947b15508751c60b73230fbd23cc8d0a64bb0fd34bfa5a54de50fa84ad0a"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.18/gestaltd-macos-x86_64.tar.gz"
      sha256 "ec3a2b596431751c03e86b2bb5de6f4b544ec97da6b4c6416a056a03e900417d"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.18/gestaltd-linux-arm64.tar.gz"
      sha256 "a068cc741393b7628be4512cf136562d43b1284b392b95c8fedb9b95c2a7cce1"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.18/gestaltd-linux-x86_64.tar.gz"
      sha256 "6538be620c9bee3fa4093fda67192ded02a3b993969466a986ed987bd5675874"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end

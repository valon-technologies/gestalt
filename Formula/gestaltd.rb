# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.10"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.10/gestaltd-macos-arm64.tar.gz"
      sha256 "d15e98455f63b95ac8e8824e8e69f670d100e5d887799e257d009a0ddf16547c"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.10/gestaltd-macos-x86_64.tar.gz"
      sha256 "424f59a4f05bade757c490d31ce1d1ca901c313c007c9bb7694a52a96f2db570"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.10/gestaltd-linux-arm64.tar.gz"
      sha256 "b1df5260efb90e5cf8eb319ebd8cd89b53c9cf17469afd1ab64aa68b7abfbe6b"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.10/gestaltd-linux-x86_64.tar.gz"
      sha256 "7dd77286d03c8a0bfc7158c1dc1d3ea990eb75d2cd4fb50badfd0412c8dca113"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end

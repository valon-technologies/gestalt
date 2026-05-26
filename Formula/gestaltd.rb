# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.6"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.6/gestaltd-macos-arm64.tar.gz"
      sha256 "2522ba4e47d7cf20a9c1e5505e203531d50f6bf16e4f31441c0171f85232abb6"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.6/gestaltd-macos-x86_64.tar.gz"
      sha256 "aea1431f3078731346d6aa44f157dcedc09734ef3d240b0172e9e4473e31a717"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.6/gestaltd-linux-arm64.tar.gz"
      sha256 "a77df93bf7e396fd2140018cc6ddcc19c5a0a31e1a61b99a10799b96138e6144"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.6/gestaltd-linux-x86_64.tar.gz"
      sha256 "ede261622739a445b88f0882e50f472002a04405e11fad17fdd651dfedf8671e"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end

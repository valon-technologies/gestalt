# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.43"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.43/gestaltd-macos-arm64.tar.gz"
      sha256 "a0e15754bfb238e4c2ace5c1c5af5190bd7aa40cb54ab172a9c7cd0a3d7bb602"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.43/gestaltd-macos-x86_64.tar.gz"
      sha256 "36a942f15fe3663cee6f6e02a71779c5b3410a8da94cdd495c05bfb4e5389abb"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.43/gestaltd-linux-arm64.tar.gz"
      sha256 "0d8424b8114775e8d56590950aa2bb7929ab6a7253d0fbf186287c3f3c2e96e6"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.43/gestaltd-linux-x86_64.tar.gz"
      sha256 "afcea4b03d9327827d651da6a4d0b9db9bd0520cee3ebf409cca5e4f28b7b754"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end

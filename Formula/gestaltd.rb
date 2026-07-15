# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.43"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.43/gestaltd-macos-arm64.tar.gz"
      sha256 "40fcb1bc36b439ddc0e4731f3a610fb79e11c7d682717a65cc7ca8bd66dd4cd0"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.43/gestaltd-macos-x86_64.tar.gz"
      sha256 "cdae5979a14fe6b2ccbae74e953b72d56b6d77cdd1a707843bcfcf5aaa73b534"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.43/gestaltd-linux-arm64.tar.gz"
      sha256 "d694a1fbc4aa9f87f864b979a4e34b3ec9ab7d72a381e25ae192fb3e4d3d9ef9"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.43/gestaltd-linux-x86_64.tar.gz"
      sha256 "85544f9f97d78ac080996e393ca22b08c8b75bdeca9f602c3274883ba9108734"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end

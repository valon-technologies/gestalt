# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.5"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.5/gestaltd-macos-arm64.tar.gz"
      sha256 "f3a4589ca0b12c2bbd69c35161d0c5b3033c7def02a362f02c72a145a05327a5"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.5/gestaltd-macos-x86_64.tar.gz"
      sha256 "0a9b26b066a09639a32a93006f7827d816b52efb255346712c70253a8f90db86"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.5/gestaltd-linux-arm64.tar.gz"
      sha256 "91ff965c4b9cafeb21a5c9cdf22560ece4ed11e92e7be48b96ebc9d0509ed000"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.5/gestaltd-linux-x86_64.tar.gz"
      sha256 "8a9b8061340e074f553bc6b1f518e6c5c64c32be96a76d887e5063c7be165e33"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end

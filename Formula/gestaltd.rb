# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.9"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.9/gestaltd-macos-arm64.tar.gz"
      sha256 "0edadc4718c1fe5ba25d679e296d5e3a20d9222730c97c2cc819b355170d65e8"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.9/gestaltd-macos-x86_64.tar.gz"
      sha256 "03dc0315c70e00541ea56436405f91d0780bde0a2b12e4cdb73de89a7e765c01"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.9/gestaltd-linux-arm64.tar.gz"
      sha256 "ec15fbc50323d57f6fdf47309a32bb9a3b203da8a77e30a1c47ac37cb71ab435"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.9/gestaltd-linux-x86_64.tar.gz"
      sha256 "1dfd62fc0095dde3bd82e1aecd5aae7cf39da53086c92f9565e0e574739794e5"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end

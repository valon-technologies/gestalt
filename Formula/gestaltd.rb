# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.24"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.24/gestaltd-macos-arm64.tar.gz"
      sha256 "8e124c97bb4167b3e51edf36fe503b965ceaa20ca31bc5894626ae1f996b42e1"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.24/gestaltd-macos-x86_64.tar.gz"
      sha256 "e52066cde4d30641144cd657be76d1a69e00db19d49986a34fad4c6084225faa"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.24/gestaltd-linux-arm64.tar.gz"
      sha256 "9acde3c4ef79def9fdd2aec8d45e49cdfdc425ce0ec96c029befc0e26e1681ca"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.24/gestaltd-linux-x86_64.tar.gz"
      sha256 "21f2e03c0378b79e55d7df46096bc67eb40d75ee04dbb630d79dc2a76146904e"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end

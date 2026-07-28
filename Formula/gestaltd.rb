# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.49"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.49/gestaltd-macos-arm64.tar.gz"
      sha256 "15e68f838367b733dd71794fe5528b6df0bf7a5f607181b218f5604249352e02"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.49/gestaltd-macos-x86_64.tar.gz"
      sha256 "2896a72cad6615ab487d0188aa91e687d3325ed8023f34b10688532cf1622c65"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.49/gestaltd-linux-arm64.tar.gz"
      sha256 "6a64b94fbb9cc063ee758cb98044b2bb4600d80591bec59681ae8684eddfc61a"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.49/gestaltd-linux-x86_64.tar.gz"
      sha256 "0bdf1f6cd07c9ef06eb0218ca4ce2f01f6d102a8b0af0beafb0640e873ad490a"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end

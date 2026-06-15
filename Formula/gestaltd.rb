# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.16"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.16/gestaltd-macos-arm64.tar.gz"
      sha256 "da6b4246626cac8684a1f515bb34911df5d7f27939c595ef1ebfe3cd5f22d85c"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.16/gestaltd-macos-x86_64.tar.gz"
      sha256 "017383415fa1f7cb79f9c6a529fde59837a671eab2c7537cdc7d16a065846db5"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.16/gestaltd-linux-arm64.tar.gz"
      sha256 "e7cc837a1c8259222bec587e72b42043335d577c761157fffd7eb6ccd5c22c6f"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.16/gestaltd-linux-x86_64.tar.gz"
      sha256 "42b698f682723d58a7aaad481b32a5ce43590b523d803434c37ef075ce950bb4"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
